package identity

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/primandproper/platform-go/v13/clock"
	"github.com/primandproper/platform-go/v13/database"
	"github.com/primandproper/platform-go/v13/database/dialect"
	platformerrors "github.com/primandproper/platform-go/v13/errors"
	"github.com/primandproper/platform-go/v13/identifiers"
	"github.com/primandproper/platform-go/v13/identity/migrations"
	"github.com/primandproper/platform-go/v13/observability"
	"github.com/primandproper/platform-go/v13/observability/logging"
	"github.com/primandproper/platform-go/v13/observability/metrics"
	"github.com/primandproper/platform-go/v13/observability/tracing"
	"github.com/primandproper/platform-go/v13/tenancy"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// DefaultTablePrefix is the namespace the identity tables carry when none is
// configured, which is none — rendering identity_users and its six siblings.
//
// The identity_ segment is the schema's, not the caller's: a table always says
// which package created it. Setting a namespace of "ddb" renders
// ddb_identity_users, for a database shared between applications. A namespace
// must not end in '_'; database/ddl supplies the separator.
const DefaultTablePrefix = ""

// storeName scopes the store's spans and logger.
const storeName = serviceName + "_store"

var _ Store = (*SQLStore)(nil)

// SQLStore is the SQL-backed Store, against the schema identity/migrations
// renders.
//
// It is exported, and returned by NewSQLStore, so a caller who has chosen SQL
// storage can depend on that choice rather than on the Store seam every backing
// shares.
type SQLStore struct {
	client database.Client
	tables *tables
	o11y   observability.Observer
	clock  clock.Clock

	unreportedRowsCounter metrics.Int64Counter

	// What the options wrote, kept only until the observer is built from it.
	// Read s.o11y.Logger() for the logger this store actually uses; this one
	// may be nil, because supplying none is how a caller asks for no logging.
	logger          logging.Logger
	tracerProvider  tracing.Provider
	metricsProvider metrics.Provider
	dialect         dialect.Dialect
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
		return nil, platformerrors.Wrapf(dialect.ErrUnsupported, "identity dialect %q", d)
	}

	s := &SQLStore{
		client:  client,
		dialect: d,
		clock:   clock.NewClock(),
		tables:  newTables(DefaultTablePrefix),
	}

	for _, opt := range opts {
		if opt != nil {
			opt(s)
		}
	}

	if err := migrations.ValidatePrefix(s.tables.prefix()); err != nil {
		return nil, err
	}

	s.o11y = observability.NewObserver(storeName, s.logger, s.tracerProvider)

	// One counter, and it is the one nothing above this layer can see.
	//
	// Every write here is guarded by "did this touch a row" — that is what turns
	// a write aimed at another directory into ErrUserNotFound instead of a
	// success. A driver that declines to report the count leaves the guard with
	// nothing to decide on, and the store treats that as a hit: reporting a
	// missing row for a write that probably happened is the worse answer. But it
	// means a genuinely missed write can be reported as applied, and that is
	// indistinguishable from the real thing unless somebody is counting.
	mp := metrics.EnsureMetricsProvider(s.metricsProvider)

	var err error
	if s.unreportedRowsCounter, err = mp.NewInt64Counter(storeName + "_unreported_row_counts"); err != nil {
		return nil, platformerrors.Wrap(err, "creating identity store unreported row count counter")
	}

	return s, nil
}

// storeOpAttr labels an unreported row count with the operation it happened in,
// since the places it can happen mean different things.
func storeOpAttr(operation string) metric.MeasurementOption {
	return metric.WithAttributes(attribute.String(storeOpKey, operation))
}

// now is the one clock read every write in this package goes through, in UTC —
// see queries.go on why the location matters to SQLite.
func (s *SQLStore) now() time.Time { return s.clock.Now().UTC() }

// notFound maps a driver's empty-result error onto this package's sentinel for
// the entity that was missing, leaving anything else alone.
//
// A read that found nothing and a read that failed are different answers, and
// collapsing them is how "the database was unreachable" gets reported to a user
// as "no such account". The sentinel is per-entity because the caller's next
// move differs: a missing user during sign-in is a rejection, a missing
// membership is an authorization failure.
func notFound(err, sentinel error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return sentinel
	}

	return err
}

// requireExecutor is the guard on every method that runs inside the caller's
// transaction. A nil executor there is a wiring mistake, and letting it reach a
// method call means a nil dereference three frames down rather than the name of
// the parameter that was not supplied.
func requireExecutor(q database.SQLQueryExecutor) error {
	if q == nil {
		return ErrNilExecutor
	}

	return nil
}

// rolesFor reads the roles for a batch of memberships or invitations at once,
// keyed by owner ID.
//
// Batched deliberately: a roster page of thirty members read one query at a time
// is thirty round trips returning two rows each, and that is the shape a page
// read arrives at when roles are fetched inside the loop that converts rows.
// An empty ownerIDs returns an empty map without querying, because an IN () is
// a syntax error in two of the three dialects.
func (s *SQLStore) rolesFor(
	ctx context.Context,
	q database.SQLQueryExecutor,
	table, idColumn string,
	ownerIDs []string,
) (map[string][]string, error) {
	byOwner := map[string][]string{}
	if len(ownerIDs) == 0 {
		return byOwner, nil
	}

	query, args := buildSelectRoles(s.dialect, table, idColumn, ownerIDs)

	type roleRow struct {
		ownerID string
		role    string
	}

	rows, err := database.ScanAll(ctx, q, "identity role", query, args, func(scanner database.Scanner) (roleRow, error) {
		var row roleRow
		if scanErr := scanner.Scan(&row.ownerID, &row.role); scanErr != nil {
			return roleRow{}, scanErr
		}

		return row, nil
	})
	if err != nil {
		return nil, err
	}

	for i := range rows {
		byOwner[rows[i].ownerID] = append(byOwner[rows[i].ownerID], rows[i].role)
	}

	return byOwner, nil
}

// replaceRoles clears an owner's roles and writes the new set, which is how both
// role tables are written — SetMembershipRoles replaces rather than merges, and
// so does the write behind an accepted invitation.
func (s *SQLStore) replaceRoles(
	ctx context.Context,
	q database.SQLQueryExecutor,
	table, idColumn, ownerID string,
	roles []string,
) error {
	query, args := buildDeleteRoles(s.dialect, table, idColumn, ownerID)
	if _, err := q.ExecContext(ctx, query, args...); err != nil {
		return platformerrors.Wrap(err, "clearing identity roles")
	}

	if len(roles) == 0 {
		return nil
	}

	query, args = buildInsertRoles(s.dialect, table, idColumn, ownerID, roles)
	if _, err := q.ExecContext(ctx, query, args...); err != nil {
		return platformerrors.Wrap(err, "writing identity roles")
	}

	return nil
}

// ensureUnique runs the collision check the unique indexes back up.
//
// The index is what actually guarantees uniqueness; this read is what turns the
// ordinary case into ErrUsernameTaken instead of a driver-specific constraint
// violation the caller would have to parse a SQLSTATE out of. Two registrations
// racing for the same handle still reach the index, and the loser gets the
// driver's error — which is correct, rare, and the reason this check is not
// presented as the guarantee.
func (s *SQLStore) ensureUnique(
	ctx context.Context,
	q database.SQLQueryExecutor,
	column string,
	scope tenancy.Scope,
	value, exceptID string,
	taken error,
) error {
	query, args := s.tables.buildSelectUserIDByField(s.dialect, column, scope, value, exceptID)

	var existingID string

	switch err := q.QueryRowContext(ctx, query, args...).Scan(&existingID); {
	case errors.Is(err, sql.ErrNoRows):
		return nil
	case err != nil:
		return platformerrors.Wrap(err, "checking identity uniqueness")
	default:
		return taken
	}
}

// newID returns the identifier a write should carry: the one the caller set, or
// a fresh one.
//
// Accepting a caller-supplied ID matters for the registration flow, where the
// user ID is often minted before the transaction so that an outbox message or a
// search document can reference it.
func newID(existing string) string {
	if existing != "" {
		return existing
	}

	return identifiers.New()
}
