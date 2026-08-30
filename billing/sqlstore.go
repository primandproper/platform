package billing

import (
	"context"
	"database/sql"
	"errors"

	"github.com/primandproper/platform-go/v13/billing/internal/billingdb"
	"github.com/primandproper/platform-go/v13/billing/migrations"
	"github.com/primandproper/platform-go/v13/clock"
	"github.com/primandproper/platform-go/v13/database"
	"github.com/primandproper/platform-go/v13/database/ddl"
	"github.com/primandproper/platform-go/v13/database/dialect"
	platformerrors "github.com/primandproper/platform-go/v13/errors"
	"github.com/primandproper/platform-go/v13/filtering"
	"github.com/primandproper/platform-go/v13/observability"
	"github.com/primandproper/platform-go/v13/observability/logging"
	"github.com/primandproper/platform-go/v13/observability/metrics"
	"github.com/primandproper/platform-go/v13/observability/tracing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// DefaultTablePrefix is the namespace the billing tables carry when none is
// configured, which is none — rendering billing_products, billing_subscriptions,
// billing_purchases and billing_transactions.
//
// The billing_ segment is the schema's, not the caller's: a table always says
// which package created it. Setting a namespace of "ddb" renders
// ddb_billing_products, for a database shared between applications. A namespace
// must not end in '_'; database/ddl supplies the separator.
const DefaultTablePrefix = ""

// storeName scopes the store's spans and logger.
const storeName = serviceName + "_store"

var _ Store = (*SQLStore)(nil)

// SQLStore is the SQL-backed Store, against the schema billing/migrations
// renders.
//
// It is exported, and returned by NewSQLStore, so a caller who has chosen SQL
// storage can depend on that choice rather than on the Store seam every backing
// shares.
//
// # One note on MySQL, because it changes what an error means
//
// The generated querier reports :execrows as rows *changed* on MySQL and rows
// *matched* on the other two. Every guarded write here discriminates in its own
// predicate — a status write requires the status to differ, a completion
// requires completed_at to be NULL — so those behave identically on all three.
// UpdateProduct and UpdateSubscription do not: on MySQL, saving a value
// identical to the one already stored reports zero rows and therefore surfaces
// as a not-found. A deployment that wants the other reading sets
// clientFoundRows=true in its MySQL DSN, which is the switch that puts MySQL on
// matched semantics.
type SQLStore struct {
	client database.Client
	q      billingdb.Querier
	o11y   observability.Observer

	clock clock.Clock

	transactionsCounter metrics.Int64Counter

	// What the options wrote, kept only until the observer is built from it.
	// Read s.o11y.Logger() for the logger this store actually uses; this one may
	// be nil, because supplying none is how a caller asks for no logging.
	logger          logging.Logger
	tracerProvider  tracing.Provider
	metricsProvider metrics.Provider
	prefix          string
}

// NewSQLStore builds a Store over the given database.
//
// The dialect comes from the client, so the two cannot disagree. The prefix must
// still match the one the migrations were rendered with — nothing here can check
// that, and a mismatch surfaces as a missing table on the first query rather
// than at construction.
//
// Observability is optional and defaults to nothing: an unconfigured store logs
// to a noop logger and traces to a noop provider.
func NewSQLStore(client database.Client, opts ...SQLStoreOption) (*SQLStore, error) {
	if client == nil {
		return nil, ErrNilDatabaseClient
	}

	d := client.Dialect()
	if !d.Valid() {
		return nil, platformerrors.Wrapf(dialect.ErrUnsupported, "billing dialect %q", d)
	}

	s := &SQLStore{
		client: client,
		prefix: DefaultTablePrefix,
		clock:  defaultClock(),
	}

	for _, opt := range opts {
		if opt != nil {
			opt(s)
		}
	}

	if err := migrations.ValidatePrefix(s.prefix); err != nil {
		return nil, err
	}

	// The generated querier, instantiated once the prefix is settled and the
	// dialect is known — the only two things the generated statements do not
	// already carry. What executes is what sqlc analyzed, with one marker
	// substitution; see billing/internal/billingdb.
	qd, err := billingdbDialect(d)
	if err != nil {
		return nil, err
	}

	q, err := billingdb.New(qd, ddl.Qualify(s.prefix))
	if err != nil {
		return nil, platformerrors.Wrap(err, "building the billing querier")
	}

	s.q = q

	s.o11y = observability.NewObserver(storeName, s.logger, s.tracerProvider)

	// One instrument, and it is the one nothing above this layer can see.
	//
	// A payment attempt either settles, fails, or is given back, and which of
	// those happened is invisible to a caller that writes a row and carries on.
	// The proportions are the health of a payment integration: a rate of
	// failures is a card form that has stopped working or a provider having a
	// bad afternoon, and a rate of refunds is a number somebody has to answer
	// for. Neither shows up in a request-level metric, because both are recorded
	// from a webhook nobody is waiting on.
	mp := metrics.EnsureMetricsProvider(s.metricsProvider)

	if s.transactionsCounter, err = mp.NewInt64Counter(storeName + "_transactions"); err != nil {
		return nil, platformerrors.Wrap(err, "creating billing store transactions counter")
	}

	return s, nil
}

// TablePrefix returns the namespace this store's tables carry, for a caller that
// needs the rendered names — a maintenance TRUNCATE, a schema audit. Pass it to
// migrations.Tables.
func (s *SQLStore) TablePrefix() string { return s.prefix }

// countTransaction records a ledger row reaching a status, including the one it
// is written at.
func (s *SQLStore) countTransaction(ctx context.Context, status TransactionStatus) {
	s.transactionsCounter.Add(ctx, 1, metric.WithAttributes(attribute.String(statusKey, string(status))))
}

// notFound maps a driver's empty-result error onto this package's sentinel for
// the entity that was missing, leaving anything else alone.
//
// A read that found nothing and a read that failed are different answers, and
// collapsing them is how "the database was unreachable" gets reported to
// somebody as "no such subscription". The sentinel is per-entity because the
// caller's next move differs: a missing product is a broken catalog reference,
// and a missing transaction is the ordinary case of an event arriving before the
// charge it describes.
func notFound(err, sentinel error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return sentinel
	}

	return err
}

// guardCount turns "the statement touched nothing" into the sentinel for the row
// that was not there, or for the guard that was not satisfied.
//
// Every predicate here includes the scope, so without this a write aimed at
// another tenant's row reports success. It is also how a guarded status write
// answers: the count, not a read before it, decides whether this caller is the
// one that moved the row.
func guardCount(count int64, err, missing error, operation string) error {
	if err != nil {
		return platformerrors.Wrap(err, operation)
	}

	if count == 0 {
		return missing
	}

	return nil
}

// pageFilter bounds a caller's filter, defaulting a missing one.
//
// The page size is what the generated statements bind, and MySQL's LIMIT takes a
// value rather than an expression — so an unset one has to become a number here
// rather than in a COALESCE the SQL cannot spell. Clamping is filtering's, at
// filtering's ceiling, which is the same treatment the URL parameter gets.
func pageFilter(filter *filtering.QueryFilter) *filtering.QueryFilter {
	if filter == nil {
		return filtering.DefaultQueryFilter()
	}

	bounded := *filter

	size := uint16(filtering.DefaultQueryFilterLimit)
	if bounded.MaxResponseSize != nil {
		size = filtering.ClampResponseSize(uint64(*bounded.MaxResponseSize))
	}

	bounded.MaxResponseSize = &size

	return &bounded
}

// billingdbDialect maps this module's dialect names onto the generated package's.
// The set is closed on both sides — NewSQLStore has already rejected anything
// d.Valid() declines — so the default arm is reachable only when this module
// learns a dialect the generated package was not generated for. That is a
// construction failure like any other, and it names the dialect, rather than
// panicking or leaning on billingdb.New refusing the empty string.
func billingdbDialect(d dialect.Dialect) (billingdb.Dialect, error) {
	switch d {
	case dialect.Postgres:
		return billingdb.DialectPostgreSQL, nil
	case dialect.MySQL:
		return billingdb.DialectMySQL, nil
	case dialect.SQLite:
		return billingdb.DialectSQLite, nil
	default:
		return "", platformerrors.Wrapf(dialect.ErrUnsupported, "no generated billing queries for dialect %q", d)
	}
}
