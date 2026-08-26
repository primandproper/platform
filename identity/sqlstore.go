package identity

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/primandproper/platform-go/v13/clock"
	"github.com/primandproper/platform-go/v13/database"
	"github.com/primandproper/platform-go/v13/database/ddl"
	"github.com/primandproper/platform-go/v13/database/dialect"
	platformerrors "github.com/primandproper/platform-go/v13/errors"
	"github.com/primandproper/platform-go/v13/filtering"
	"github.com/primandproper/platform-go/v13/identifiers"
	"github.com/primandproper/platform-go/v13/identity/internal/identitydb"
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
	q      identitydb.Querier
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

	// The generated querier, instantiated once the prefix is settled and the
	// dialect is known — the only two things the generated statements do not
	// already carry. What executes is what sqlc analyzed, with one marker
	// substitution; see identity/internal/identitydb.
	qd, err := identitydbDialect(d)
	if err != nil {
		return nil, err
	}

	q, err := identitydb.New(qd, ddl.Qualify(s.tables.prefix()))
	if err != nil {
		return nil, platformerrors.Wrap(err, "building the identity querier")
	}

	s.q = q

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

// What follows is the machinery the nine implementation files share — the
// column names spelled in more than one of them, the reads two seams both need,
// and the two guards every paged read and every guarded write goes through.
// Anything reached from one file only lives in that file.

// The user columns the collision checks and the sign-in reads name. They are
// constants rather than literals at the call sites because each is spelled in
// two places — a builder's predicate and the method that calls it — and a typo
// in one of them is a query that compiles, runs, and matches nothing.
const (
	usernameColumn     = "username"
	emailAddressColumn = "email_address"
	emailTokenColumn   = "email_address_verification_token"
)

// userIDColumn is what the service-role table keys on.
const userIDColumn = "user_id"

// membershipIDColumn is what the membership role table keys on.
const membershipIDColumn = "membership_id"

// liveUserBy is the one implementation behind the three single-user reads that
// must exclude archived users. They differ in one column and nothing else, and
// the parts that must not differ — the scope predicate and the archived clause —
// are written once here.
func (s *SQLStore) liveUserBy(ctx context.Context, column string, scope tenancy.Scope, value, description string) (*User, error) {
	ctx, op := s.o11y.Begin(ctx, observability.WithValue(scopeKey, scope.String()))
	defer op.End()

	if err := scope.Validate(); err != nil {
		return nil, op.Error(err, "%s", description)
	}

	query, args := s.tables.buildSelectLiveUserBy(s.dialect, column, scope, value)

	user, err := scanUser(s.client.Reader().QueryRowContext(ctx, query, args...))
	if err != nil {
		return nil, op.Error(notFound(err, ErrUserNotFound), "%s", description)
	}

	if err = s.attachServiceRoles(ctx, s.client.Reader(), []*User{user}); err != nil {
		return nil, op.Error(err, "%s", description)
	}

	op.SpanOnly(userIDKey, user.ID)

	return user, nil
}

// attachServiceRoles fills in the ServiceRoles of a batch of users with one
// query, rather than one per user — which is what a directory page would
// otherwise cost.
func (s *SQLStore) attachServiceRoles(ctx context.Context, q database.SQLQueryExecutor, users []*User) error {
	ids := make([]string, 0, len(users))
	for _, user := range users {
		ids = append(ids, user.ID)
	}

	byUser, err := s.rolesFor(ctx, q, s.tables.userRoles, userIDColumn, ids)
	if err != nil {
		return err
	}

	for _, user := range users {
		user.ServiceRoles = byUser[user.ID]
	}

	return nil
}

// readMembershipsForUser is the read behind both ListMembershipsForUser and
// GetPrincipal, roles attached.
func (s *SQLStore) readMembershipsForUser(
	ctx context.Context,
	q database.SQLQueryExecutor,
	scope tenancy.Scope,
	userID string,
) ([]*Membership, error) {
	rows, err := s.q.ListMembershipsForUser(ctx, q, listMembershipsForUserParams(scope, userID))
	if err != nil {
		return nil, err
	}

	memberships := make([]*Membership, 0, len(rows))
	for i := range rows {
		memberships = append(memberships, membershipFromRow(&rows[i]))
	}

	if err = s.attachMembershipRoles(ctx, q, memberships); err != nil {
		return nil, err
	}

	return memberships, nil
}

// attachMembershipRoles fills in the Roles of a batch of memberships with one
// query, rather than one per membership.
func (s *SQLStore) attachMembershipRoles(ctx context.Context, q database.SQLQueryExecutor, memberships []*Membership) error {
	ids := make([]string, 0, len(memberships))
	for _, membership := range memberships {
		ids = append(ids, membership.ID)
	}

	byMembership, err := s.rolesFor(ctx, q, s.tables.membershipRoles, membershipIDColumn, ids)
	if err != nil {
		return err
	}

	for _, membership := range memberships {
		membership.Roles = byMembership[membership.ID]
	}

	return nil
}

// writeMembership upserts the membership row, resolves the ID the row actually
// carries, and replaces its roles.
//
// The ID is read back rather than assumed because the upsert may have taken its
// conflict branch — a user rejoining an account revives the archived membership,
// which keeps the ID it was created with. Writing the roles against the ID the
// caller generated would attach them to a membership that does not exist.
func (s *SQLStore) writeMembership(ctx context.Context, q database.SQLQueryExecutor, membership *Membership) error {
	query, args := s.tables.buildUpsertMembership(s.dialect, membership, membership.CreatedAt)
	if _, err := q.ExecContext(ctx, query, args...); err != nil {
		return platformerrors.Wrap(err, "writing identity membership")
	}

	query, args = s.tables.buildSelectMembershipID(s.dialect, membership.BelongsToUser, membership.BelongsToAccount)
	if err := q.QueryRowContext(ctx, query, args...).Scan(&membership.ID); err != nil {
		return platformerrors.Wrap(err, "reading back identity membership")
	}

	if err := s.replaceRoles(ctx, q, s.tables.membershipRoles, membershipIDColumn, membership.ID, membership.Roles); err != nil {
		return err
	}

	if !membership.DefaultAccount {
		return nil
	}

	query, args = s.tables.buildClearDefaultAccount(
		s.dialect, membership.Scope, membership.BelongsToUser, membership.BelongsToAccount, membership.CreatedAt,
	)
	if _, err := q.ExecContext(ctx, query, args...); err != nil {
		return platformerrors.Wrap(err, "clearing other identity default accounts")
	}

	return nil
}

// liveMembershipCount counts a user's live memberships, for deciding whether the
// one being written is their first.
func (s *SQLStore) liveMembershipCount(ctx context.Context, q database.SQLQueryExecutor, scope tenancy.Scope, userID string) (int, error) {
	query, args := s.tables.buildCountLiveMembershipsForUser(s.dialect, scope, userID)

	var count int
	if err := q.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		return 0, platformerrors.Wrap(err, "counting identity memberships")
	}

	return count, nil
}

// pageFilter is the filter a paged read is answered under: the caller's, with
// the page-size ceiling every other paged read in this module applies.
//
// It works on a copy. The clamp has to be applied to what the query binds and
// to what the result reports, and doing that by writing through the caller's
// pointer would hand them back a filter they did not pass — a store that
// rewrites its argument is a store whose caller cannot reuse one.
//
// A page size that is present and zero is left alone and returns no rows, which
// is the loud reading of an explicit zero and the same distinction
// filtering.ClampResponseSize draws. Only absence is defaulted.
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

// pageWindow reads the cursor and limit a hand-written page query binds out of
// a filter, through the same clamp the rendered ones go through.
//
// One read is left that needs it: the username prefix search, whose pattern,
// ESCAPE clause and ordering by username are its own, so it takes the cursor and
// the limit as ordinary arguments rather than through the generated params. The
// two junction reads used to be here too — see identity/internal/queries.
func pageWindow(filter *filtering.QueryFilter) (normalized *filtering.QueryFilter, cursor string, limit int) {
	normalized = pageFilter(filter)

	if normalized.Cursor != nil {
		cursor = *normalized.Cursor
	}

	return normalized, cursor, int(*normalized.MaxResponseSize)
}

// identitydbDialect maps this module's dialect names onto the generated
// package's. The set is closed on both sides — NewSQLStore has already
// rejected anything d.Valid() declines — so the default arm is reachable only
// when this module learns a dialect the generated package was not generated
// for. That is a construction failure like any other, and it names the
// dialect, rather than panicking or leaning on identitydb.New refusing the
// empty string.
func identitydbDialect(d dialect.Dialect) (identitydb.Dialect, error) {
	switch d {
	case dialect.Postgres:
		return identitydb.DialectPostgreSQL, nil
	case dialect.MySQL:
		return identitydb.DialectMySQL, nil
	case dialect.SQLite:
		return identitydb.DialectSQLite, nil
	default:
		return "", platformerrors.Wrapf(dialect.ErrUnsupported, "no generated identity queries for dialect %q", d)
	}
}

// guardCount is execExpectingRow's generated-statement half. The statement ran
// through identitydb, whose :execrows methods already asked the driver for the
// affected count, so what is left is mapping "touched nothing" onto the
// sentinel for the entity that was not there — every predicate here includes
// the scope, and without this a write aimed at another directory's row reports
// success.
//
// One narrowing against the hand-written path: a driver that declines to
// report the count reaches this as an error rather than as an acknowledged
// unknown, because the generated method has no seam between running the
// statement and reading the count. None of the three supported drivers
// declines; the old tolerance guarded a hypothetical.
func guardCount(count int64, err, missing error, operation string) error {
	if err != nil {
		return platformerrors.Wrap(err, operation)
	}

	if count == 0 {
		return missing
	}

	return nil
}

// stampCreatedAt reads back the creation time the database assigned and writes
// it onto the value the caller handed over.
//
// The column is the database's — see identity/internal/queries — so the create
// does not carry it, and the alternative to this read is a caller whose struct
// says 0001-01-01 for a row that was written a moment ago. That is the worse
// answer by some distance: CreatedAt is exported and a service serializes the
// value it just created straight into a response, where a zero time renders as
// a date rather than reading as an absence. So the create reads it back, and
// the field means what it says on both sides of the call.
//
// It costs one round trip inside a transaction the write already needed, and it
// reads its own uncommitted row on all three servers.
func (s *SQLStore) stampCreatedAt(
	ctx context.Context,
	q database.SQLQueryExecutor,
	table, id string,
	at *time.Time,
) error {
	query, args := s.tables.buildSelectCreatedAt(s.dialect, table, id)

	var created time.Time
	if err := q.QueryRowContext(ctx, query, args...).Scan(&created); err != nil {
		return platformerrors.Wrap(err, "reading back the assigned creation time")
	}

	*at = created.UTC()

	return nil
}

// execExpectingRow runs a write that must touch a row, mapping "touched
// nothing" onto the sentinel for the entity that was not there.
//
// It exists because an UPDATE whose predicate matched nothing is a success as
// far as the driver is concerned, and every predicate here includes the scope.
// Without this, a write aimed at another directory's user returns nil — the
// caller is told their change was applied, to a row that does not exist as far
// as they are concerned.
//
// A driver that declines to report the count is treated as a hit and counted,
// because the alternative is reporting a missing row for a write that probably
// happened. The count is the only way that assumption is visible: see
// NewSQLStore.
func (s *SQLStore) execExpectingRow(
	ctx context.Context,
	op observability.Operation,
	q database.SQLQueryExecutor,
	query string,
	args []any,
	missing error,
	operation string,
) error {
	result, err := q.ExecContext(ctx, query, args...)
	if err != nil {
		return platformerrors.Wrap(err, operation)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		// Acknowledged rather than returned, and counted, because from here the
		// write is indistinguishable from one that matched a row.
		op.Acknowledge(err, "reading rows affected by %s", operation)
		s.unreportedRowsCounter.Add(ctx, 1, storeOpAttr(operation))

		return nil
	}

	if affected == 0 {
		return missing
	}

	return nil
}
