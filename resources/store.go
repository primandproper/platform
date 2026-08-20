package resources

import (
	"context"
	"database/sql"
	"errors"
	"slices"

	"github.com/primandproper/platform-go/v12/database"
	"github.com/primandproper/platform-go/v12/database/dialect"
	"github.com/primandproper/platform-go/v12/database/querygen"
	platformerrors "github.com/primandproper/platform-go/v12/errors"
	"github.com/primandproper/platform-go/v12/filtering"
	"github.com/primandproper/platform-go/v12/observability"
	"github.com/primandproper/platform-go/v12/observability/metrics"
	"github.com/primandproper/platform-go/v12/observability/tracing"
	"github.com/primandproper/platform-go/v12/tenancy"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// ErrNoRowsAffected indicates a write whose statement matched nothing: the row
// is gone, or it is not this actor's, or it is not in this scope.
//
// The three are one error on purpose. Distinguishing "no such row" from "not
// yours" tells an unauthorized caller which ids exist, which is the enumeration
// the owner predicate was there to prevent.
var ErrNoRowsAffected = platformerrors.New("no rows affected")

// instrumentPrefix is the name the store's instruments are built under.
//
// It is the kit's name rather than the resource's, and the resource is an
// attribute instead. Naming the instruments per resource would put every domain
// in an application in a different time series, so "which resource is failing"
// would be a question about which panels exist rather than one a single panel
// answers — and a new domain would arrive invisible until someone added its
// chart. See metrics.OperationSet for what the three instruments are.
const instrumentPrefix = "resources"

// The attribute keys the store labels its instruments and spans with.
const (
	resourceAttribute = "resource"
	methodAttribute   = "resource.method"
)

// method names one of the store's operations, for the span name and the metric
// attribute.
type method string

const (
	methodGet             method = "get"
	methodGetMany         method = "get_many"
	methodExists          method = "exists"
	methodList            method = "list"
	methodCreate          method = "create"
	methodUpdate          method = "update"
	methodArchive         method = "archive"
	methodArchiveMatching method = "archive_matching"
)

// everyMethod is what the per-method measurement options are built from at
// construction, so that no call site assembles an attribute set of its own.
func everyMethod() []method {
	return []method{
		methodGet, methodGetMany, methodExists, methodList,
		methodCreate, methodUpdate, methodArchive, methodArchiveMatching,
	}
}

// Store is the runtime a Resource is served through: the declaration, a database
// client, and whatever hooks the application attached.
//
// Its methods are a bind and an execute. Every statement but the set read was
// rendered at construction, so nothing here builds SQL and nothing here can
// build SQL that differs from what the declaration said — see
// Resource.getManyFor for the one statement whose arity belongs to its caller.
//
// It is a concrete type and there is deliberately no Store interface here. A
// service that depends on one wants the four or five methods it actually calls,
// declared on its own side where the compiler can check that it uses no more
// than it asked for; an interface declared here would be the union of every
// method every consumer needs, and a consumer's test double would have to
// grow a method every time this package did. See the package documentation.
type Store[T any] struct {
	resource *Resource[T]
	client   database.Client
	observer observability.Observer

	instruments *metrics.OperationSet
	// measurements holds the attribute set each method records under, built
	// once so that a call site cannot assemble a different one.
	measurements map[method]metric.MeasurementOption

	newID func() string
	hooks []Hook[T]
}

// NewStore builds a Store for a resource over a database client.
//
// The dialect is the client's, and the resource was rendered for one, so the two
// are checked against each other here: a resource rendered for Postgres served
// through a MySQL client is SQL that parses on neither, and catching it at
// construction is the difference between a startup failure and a runtime one.
func NewStore[T any](resource *Resource[T], client database.Client, opts ...Option) (*Store[T], error) {
	if resource == nil || client == nil {
		return nil, platformerrors.ErrNilInputParameter
	}

	if resource.generator.Dialect() != client.Dialect() {
		return nil, platformerrors.Wrapf(
			dialect.ErrUnsupported,
			"resources: %s was rendered for %s but the client speaks %s",
			resource.def.Name, resource.generator.Dialect(), client.Dialect(),
		)
	}

	settings := &options{newID: defaultIDFactory}

	for _, opt := range opts {
		if opt != nil {
			opt(settings)
		}
	}

	hooks := make([]Hook[T], 0, len(settings.hooks))

	for _, hook := range settings.hooks {
		typed, ok := hook.(Hook[T])
		if !ok {
			return nil, platformerrors.Wrapf(ErrHookTypeMismatch, "resources: %s was given a %T", resource.def.Name, hook)
		}

		hooks = append(hooks, typed)
	}

	instruments, err := metrics.NewOperationSet(settings.metricsProvider, instrumentPrefix)
	if err != nil {
		return nil, platformerrors.Wrapf(err, "resources: instrumenting %s", resource.def.Name)
	}

	measurements := make(map[method]metric.MeasurementOption, len(everyMethod()))
	for _, name := range everyMethod() {
		measurements[name] = metric.WithAttributes(
			attribute.String(resourceAttribute, resource.def.Name),
			attribute.String(methodAttribute, string(name)),
		)
	}

	return &Store[T]{
		resource:     resource,
		client:       client,
		newID:        settings.newID,
		hooks:        hooks,
		instruments:  instruments,
		measurements: measurements,
		observer: observability.NewObserverWithValues(
			resource.def.Name+"_store",
			settings.logger,
			settings.tracerProvider,
			map[string]any{resourceAttribute: resource.def.Name},
		),
	}, nil
}

// begin starts a call's span and its measurement, and returns the function that
// ends both.
//
// The returned function takes the address of the method's error rather than its
// value, so that what it records is what the method finally returned. That is
// the whole reason this exists rather than a Failed call at each return: a store
// method has a dozen of them, and the one somebody forgets is not a missing
// metric but a wrong one — an error rate that reads lower than it is, on the
// path that failed.
//
// It names the span explicitly rather than letting Observer.Begin resolve it
// from the caller, because Begin reads the frame directly below it and this is
// that frame. A span named for this function is a trace where every operation is
// called "begin".
func (s *Store[T]) begin(ctx context.Context, name method) (context.Context, observability.Operation, func(*error)) {
	ctx, op := s.observer.BeginCustom(ctx, s.resource.def.Name+"_store."+string(name))
	op.SpanOnly(methodAttribute, string(name))

	measurement := s.measurements[name]

	s.instruments.Attempt(ctx, measurement)

	stop := op.Time(ctx, nil, s.instruments.Latency, measurement)

	return ctx, op, func(err *error) {
		stop()

		if err != nil && *err != nil {
			s.instruments.Failed(ctx, measurement)
		}

		op.End()
	}
}

// Get reads one row by id.
func (s *Store[T]) Get(ctx context.Context, scope tenancy.Scope, actor Actor, id string) (row *T, err error) {
	ctx, op, done := s.begin(ctx, methodGet)
	defer done(&err)

	if err = s.admit(op, scope, actor, id); err != nil {
		return nil, err
	}

	values := s.readValues(scope, actor)

	values[querygen.IDColumn] = id

	statement := s.resource.as(actor).get

	args, err := statement.Bind(values)
	if err != nil {
		return nil, op.Error(err, "binding %s read", s.resource.def.Name)
	}

	row = new(T)

	if err = s.client.Reader().QueryRowContext(ctx, statement.SQL, args...).Scan(s.resource.scanTargets(row)...); err != nil {
		return nil, op.Error(err, "reading %s", s.resource.def.Name)
	}

	return row, nil
}

// GetMany reads a set of rows by id, in one statement.
//
// It is not a page and takes no filter: a caller naming n ids has already
// decided how many rows there are, and what it wants back is those rows. They
// come back in whatever order the server produced them, because the statement
// carries no ORDER BY — a caller that needs them in the order it asked for them
// indexes what it got by id, which is what a caller resolving a set of ids is
// almost always about to do anyway.
//
// Fewer rows than ids is the ordinary answer rather than an error. An id may be
// archived, or in another scope, or belong to another owner on a resource whose
// owner gates reads, and the caller learns the same thing about all three: it is
// not among the rows. Reporting which of them it was would answer the question
// the single-row read refuses to answer — see ErrNoRowsAffected.
//
// An empty id set is not a query. It returns no rows and no error, which is what
// the statement would have said had there been one to render.
func (s *Store[T]) GetMany(ctx context.Context, scope tenancy.Scope, actor Actor, ids ...string) (rows []*T, err error) {
	ctx, op, done := s.begin(ctx, methodGetMany)
	defer done(&err)

	if err = s.admitScope(op, scope, actor); err != nil {
		return nil, err
	}

	if len(ids) == 0 {
		return []*T{}, nil
	}

	if slices.Contains(ids, "") {
		return nil, platformerrors.ErrInvalidIDProvided
	}

	op.Set("id_count", len(ids))

	statement, err := s.resource.getManyFor(actor, len(ids))
	if err != nil {
		return nil, op.Error(err, "rendering %s set read", s.resource.def.Name)
	}

	values := s.readValues(scope, actor)

	s.resource.generator.BindIDs(values, ids)

	args, err := statement.Bind(values)
	if err != nil {
		return nil, op.Error(err, "binding %s set read", s.resource.def.Name)
	}

	rows, err = database.ScanAll(ctx, s.client.Reader(), s.resource.def.Name, statement.SQL, args, func(scanner database.Scanner) (*T, error) {
		row := new(T)
		if scanErr := scanner.Scan(s.resource.scanTargets(row)...); scanErr != nil {
			return nil, scanErr
		}

		return row, nil
	})
	if err != nil {
		return nil, op.Error(err, "reading %s set", s.resource.def.Name)
	}

	if rows == nil {
		rows = []*T{}
	}

	return rows, nil
}

// Exists reports whether Get would find a row, without reading it.
func (s *Store[T]) Exists(ctx context.Context, scope tenancy.Scope, actor Actor, id string) (found bool, err error) {
	ctx, op, done := s.begin(ctx, methodExists)
	defer done(&err)

	if err = s.admit(op, scope, actor, id); err != nil {
		return false, err
	}

	values := s.readValues(scope, actor)

	values[querygen.IDColumn] = id

	statement := s.resource.as(actor).exists

	args, err := statement.Bind(values)
	if err != nil {
		return false, op.Error(err, "binding %s existence check", s.resource.def.Name)
	}

	if err = s.client.Reader().QueryRowContext(ctx, statement.SQL, args...).Scan(&found); err != nil {
		return false, op.Error(err, "checking %s existence", s.resource.def.Name)
	}

	return found, nil
}

// List reads a filtered, cursor-paginated page.
//
// The matches key the read, and their combination has to be one the definition
// declared as a Lookup — see ErrUndeclaredLookup. Passing none reads everything
// in scope.
func (s *Store[T]) List(ctx context.Context, scope tenancy.Scope, actor Actor, filter *filtering.QueryFilter, matches ...Match) (*filtering.QueryFilteredResult[T], error) {
	return s.listWith(ctx, s.client.Reader(), scope, actor, filter, matches...)
}

// listWith is List against a caller-supplied executor, so a cascade can read its
// doomed set inside the transaction that is about to archive it.
func (s *Store[T]) listWith(ctx context.Context, exec database.SQLQueryExecutor, scope tenancy.Scope, actor Actor, filter *filtering.QueryFilter, matches ...Match) (page *filtering.QueryFilteredResult[T], err error) {
	ctx, op, done := s.begin(ctx, methodList)
	defer done(&err)

	if err = s.admitScope(op, scope, actor); err != nil {
		return nil, err
	}

	if filter == nil {
		filter = filtering.DefaultQueryFilter()
	}

	tracing.AttachQueryFilterToSpan(op.Span(), filter)

	statement, err := s.resource.listFor(actor, matches)
	if err != nil {
		return nil, op.Error(err, "resolving %s lookup", s.resource.def.Name)
	}

	values := s.readValues(scope, actor)

	bindMatches(values, matches)

	for i := range matches {
		op.Set(matches[i].Column, matches[i].Value)
	}

	s.resource.generator.BindFilter(values, filter)

	args, err := statement.Bind(values)
	if err != nil {
		return nil, op.Error(err, "binding %s list", s.resource.def.Name)
	}

	var filteredCount, totalCount int64

	rows, err := database.ScanAll(ctx, exec, s.resource.def.Name, statement.SQL, args, func(scanner database.Scanner) (*T, error) {
		row := new(T)

		targets := append(s.resource.scanTargets(row), &filteredCount, &totalCount)
		if scanErr := scanner.Scan(targets...); scanErr != nil {
			return nil, scanErr
		}

		return row, nil
	})
	if err != nil {
		return nil, op.Error(err, "listing %s", s.resource.def.Name)
	}

	if rows == nil {
		rows = []*T{}
	}

	return filtering.NewQueryFilteredResult(
		rows,
		uint64(max(filteredCount, 0)),
		uint64(max(totalCount, 0)),
		s.resource.idOf,
		filter,
	), nil
}

// Create inserts a row and returns it as stored.
//
// The row is re-read inside the same transaction rather than assembled from what
// was sent, so the returned created_at is the server's and not the application
// process's. Two application instances whose clocks differ by a second would
// otherwise return creation times that the filter window disagrees with.
//
// That re-read is a second round trip per create, and it stays deliberately.
// Postgres and SQLite could return the stored row from the INSERT itself, but
// MySQL has no RETURNING, so taking it would mean a create statement that
// differs by dialect — and it would mean this package's create was no longer the
// statement querygen emits for the sqlc side, which is the property the whole
// arrangement rests on. A round trip inside a transaction that is already open
// is a cost worth naming and not one worth two renderings of the same write.
//
// An empty id is minted. A supplied one is kept, which is what lets a caller
// idempotently re-create a row it already named.
func (s *Store[T]) Create(ctx context.Context, scope tenancy.Scope, actor Actor, row *T) (created *T, err error) {
	ctx, op, done := s.begin(ctx, methodCreate)
	defer done(&err)

	if row == nil {
		return nil, platformerrors.ErrNilInputParameter
	}

	if err = s.admitScope(op, scope, actor); err != nil {
		return nil, err
	}

	if s.resource.idOf(row) == "" {
		s.resource.setID(row, s.newID())
	}

	id := s.resource.idOf(row)
	op.Set(s.resource.def.Name+"_id", id)

	// The scope is written from the argument rather than trusted from the row,
	// so a caller cannot create a row into a tenant it did not name.
	s.resource.setScope(row, scope)

	if s.resource.def.Validate != nil {
		if err = s.resource.def.Validate(ctx, row); err != nil {
			return nil, op.Error(err, "validating %s", s.resource.def.Name)
		}
	}

	values := s.resource.rowValues(row)

	args, err := s.resource.create.Bind(values)
	if err != nil {
		return nil, op.Error(err, "binding %s creation", s.resource.def.Name)
	}

	if err = s.client.WithTransaction(ctx, func(tx database.SQLQueryExecutor) error {
		if _, execErr := tx.ExecContext(ctx, s.resource.create.SQL, args...); execErr != nil {
			return op.Error(execErr, "creating %s", s.resource.def.Name)
		}

		stored, readErr := s.readInTransaction(ctx, tx, scope, actor, id)
		if readErr != nil {
			return readErr
		}

		created = stored

		return s.fire(ctx, tx, Change[T]{
			Op:       OpCreated,
			Resource: s.resource.def.Name,
			Table:    s.resource.def.Table,
			ID:       id,
			Owner:    s.resource.ownerOf(created),
			Scope:    scope,
			Actor:    actor,
			Row:      created,
		})
	}); err != nil {
		return nil, err
	}

	return created, nil
}

// Update reassigns every mutable column and returns the row as stored.
//
// Which columns those are is the declaration's business: the id, the scope, the
// owner and anything marked Immutable are excluded, so an update cannot move a
// row to another tenant or reattach it to another parent.
func (s *Store[T]) Update(ctx context.Context, scope tenancy.Scope, actor Actor, row *T) (updated *T, err error) {
	ctx, op, done := s.begin(ctx, methodUpdate)
	defer done(&err)

	if row == nil {
		return nil, platformerrors.ErrNilInputParameter
	}

	id := s.resource.idOf(row)

	if err = s.admit(op, scope, actor, id); err != nil {
		return nil, err
	}

	if s.resource.def.Validate != nil {
		if err = s.resource.def.Validate(ctx, row); err != nil {
			return nil, op.Error(err, "validating %s", s.resource.def.Name)
		}
	}

	statement := s.resource.as(actor).update

	values := s.resource.rowValues(row)
	s.applyPredicateValues(values, scope, actor)

	args, err := statement.Bind(values)
	if err != nil {
		return nil, op.Error(err, "binding %s update", s.resource.def.Name)
	}

	if err = s.client.WithTransaction(ctx, func(tx database.SQLQueryExecutor) error {
		result, execErr := tx.ExecContext(ctx, statement.SQL, args...)
		if execErr != nil {
			return op.Error(execErr, "updating %s", s.resource.def.Name)
		}

		if affectedErr := requireAffected(result); affectedErr != nil {
			return affectedErr
		}

		stored, readErr := s.readInTransaction(ctx, tx, scope, actor, id)
		if readErr != nil {
			return readErr
		}

		updated = stored

		return s.fire(ctx, tx, Change[T]{
			Op:       OpUpdated,
			Resource: s.resource.def.Name,
			Table:    s.resource.def.Table,
			ID:       id,
			Owner:    s.resource.ownerOf(updated),
			Scope:    scope,
			Actor:    actor,
			Row:      updated,
		})
	}); err != nil {
		return nil, err
	}

	return updated, nil
}

// Archive soft-deletes one row by id.
func (s *Store[T]) Archive(ctx context.Context, scope tenancy.Scope, actor Actor, id string) (err error) {
	ctx, op, done := s.begin(ctx, methodArchive)
	defer done(&err)

	if err = s.admit(op, scope, actor, id); err != nil {
		return err
	}

	if !s.resource.softDeletes {
		return platformerrors.Wrapf(ErrUnknownColumn, "resources: %s has no %s column", s.resource.def.Name, querygen.ArchivedAtColumn)
	}

	statement := s.resource.as(actor).archive

	values := map[string]any{querygen.IDColumn: id}
	s.applyPredicateValues(values, scope, actor)

	args, err := statement.Bind(values)
	if err != nil {
		return op.Error(err, "binding %s archival", s.resource.def.Name)
	}

	return s.client.WithTransaction(ctx, func(tx database.SQLQueryExecutor) error {
		// The row is read before it is archived, so a hook is told who owned it —
		// afterwards it is archived, and every read this package issues filters
		// archived rows out.
		//
		// A row that is already gone has to come back as ErrNoRowsAffected and
		// not as the read's own sql.ErrNoRows. The pre-read is this method's
		// implementation detail, and letting its error escape would mean
		// archiving a row twice reported something different from archiving a
		// row that never existed, purely because of the order of statements
		// inside here.
		existing, readErr := s.readInTransaction(ctx, tx, scope, actor, id)
		if readErr != nil {
			if errors.Is(readErr, sql.ErrNoRows) {
				return ErrNoRowsAffected
			}

			return readErr
		}

		result, execErr := tx.ExecContext(ctx, statement.SQL, args...)
		if execErr != nil {
			return op.Error(execErr, "archiving %s", s.resource.def.Name)
		}

		if affectedErr := requireAffected(result); affectedErr != nil {
			return affectedErr
		}

		return s.fire(ctx, tx, Change[T]{
			Op:       OpArchived,
			Resource: s.resource.def.Name,
			Table:    s.resource.def.Table,
			ID:       id,
			Owner:    s.resource.ownerOf(existing),
			Scope:    scope,
			Actor:    actor,
			Row:      existing,
		})
	})
}

// ArchiveMatching soft-deletes every row matching a declared lookup, for the
// cascades an application performs when the thing its rows hang off is deleted.
//
// It reports one Change per row, which is what lets an audit hook record the
// cascade rather than a single opaque event. The rows are read first, inside the
// transaction, so that set is the set the statement is about to archive.
//
// The owner predicate is deliberately absent: a cascade archives every author's
// rows, which is the point of it, so this requires the system actor rather than
// silently widening what a user's actor may touch.
func (s *Store[T]) ArchiveMatching(ctx context.Context, scope tenancy.Scope, actor Actor, matches ...Match) (err error) {
	ctx, op, done := s.begin(ctx, methodArchiveMatching)
	defer done(&err)

	if err = s.admitScope(op, scope, actor); err != nil {
		return err
	}

	if !actor.IsSystem() {
		return platformerrors.Wrap(platformerrors.ErrPermissionDenied, "resources: a cascade crosses owners and requires the system actor")
	}

	statement, err := s.resource.archiveMatchingFor(matches)
	if err != nil {
		return op.Error(err, "resolving %s lookup", s.resource.def.Name)
	}

	values := s.readValues(scope, actor)

	bindMatches(values, matches)

	args, err := statement.Bind(values)
	if err != nil {
		return op.Error(err, "binding %s cascade", s.resource.def.Name)
	}

	return s.client.WithTransaction(ctx, func(tx database.SQLQueryExecutor) error {
		// The doomed set is read through the caller's executor, inside the same
		// transaction as the archival. Read outside it, a row written in between
		// would be archived by the statement and reported to no hook — an
		// audit log that is missing exactly the rows a race touched.
		doomed, readErr := s.listWith(ctx, tx, scope, actor, everything(), matches...)
		if readErr != nil {
			return op.Error(readErr, "reading %s for cascade", s.resource.def.Name)
		}

		if _, execErr := tx.ExecContext(ctx, statement.SQL, args...); execErr != nil {
			return op.Error(execErr, "archiving %s", s.resource.def.Name)
		}

		for _, row := range doomed.Data {
			if fireErr := s.fire(ctx, tx, Change[T]{
				Op:       OpArchived,
				Resource: s.resource.def.Name,
				Table:    s.resource.def.Table,
				ID:       s.resource.idOf(row),
				Owner:    s.resource.ownerOf(row),
				Scope:    scope,
				Actor:    actor,
				Row:      row,
			}); fireErr != nil {
				return fireErr
			}
		}

		return nil
	})
}

// readInTransaction re-reads a row through the caller's executor, so a write and
// the row it returns come from the same transaction.
func (s *Store[T]) readInTransaction(ctx context.Context, tx database.SQLQueryExecutor, scope tenancy.Scope, actor Actor, id string) (*T, error) {
	statement := s.resource.internal().get

	values := s.readValues(scope, actor)

	values[querygen.IDColumn] = id

	args, err := statement.Bind(values)
	if err != nil {
		return nil, err
	}

	row := new(T)
	if err = tx.QueryRowContext(ctx, statement.SQL, args...).Scan(s.resource.scanTargets(row)...); err != nil {
		return nil, err
	}

	return row, nil
}

// fire runs the hooks in registration order, stopping at the first error.
func (s *Store[T]) fire(ctx context.Context, tx database.SQLQueryExecutor, change Change[T]) error {
	for _, hook := range s.hooks {
		if err := hook(ctx, tx, change); err != nil {
			return err
		}
	}

	return nil
}

// admit is admitScope plus the id check every single-row method shares.
func (s *Store[T]) admit(op observability.Operation, scope tenancy.Scope, actor Actor, id string) error {
	if err := s.admitScope(op, scope, actor); err != nil {
		return err
	}

	if id == "" {
		return platformerrors.ErrInvalidIDProvided
	}

	op.Set(s.resource.def.Name+"_id", id)

	return nil
}

// admitScope validates the two dimensions every method takes and records them.
//
// An unscoped resource refuses a scope naming a tenant rather than ignoring it.
// Ignoring it would mean a caller who believed they were reading one account's
// rows got every account's, which is the failure this whole dimension exists to
// prevent — and it would do so silently.
func (s *Store[T]) admitScope(op observability.Operation, scope tenancy.Scope, actor Actor) error {
	if err := scope.Validate(); err != nil {
		return err
	}

	if s.resource.def.Scoping == Unscoped && !scope.IsGlobal() {
		return platformerrors.Wrapf(ErrScopeNotSupported, "resources: %s", s.resource.def.Name)
	}

	if err := actor.Validate(); err != nil {
		return err
	}

	op.SetValues(map[string]any{"scope": scope.String(), "actor": actor.String()})

	return nil
}

// readValues seeds the value map with the predicate dimensions a read carries.
func (s *Store[T]) readValues(scope tenancy.Scope, actor Actor) map[string]any {
	values := map[string]any{}

	s.applyPredicateValues(values, scope, actor)

	return values
}

// applyPredicateValues binds the scope and owner columns a statement's
// predicates name.
//
// The owner is bound from the actor, never from the row: binding the row's own
// owner into its update's WHERE would compare a value against itself and gate
// nothing.
func (s *Store[T]) applyPredicateValues(values map[string]any, scope tenancy.Scope, actor Actor) {
	if s.resource.scope != nil {
		values[s.resource.scope.name] = scope
	}

	if s.resource.owner != nil && !actor.IsSystem() {
		values[s.resource.owner.name] = actor.ID()
	}
}

// requireAffected turns a write that matched nothing into ErrNoRowsAffected.
func requireAffected(result sql.Result) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return platformerrors.Wrap(err, "reading affected rows")
	}

	if affected == 0 {
		return ErrNoRowsAffected
	}

	return nil
}

// everything is the filter a cascade reads its doomed set through: every row,
// one page, as large as a page may be.
func everything() *filtering.QueryFilter {
	filter := filtering.DefaultQueryFilter()
	limit := uint16(filtering.MaxQueryFilterLimit)
	filter.MaxResponseSize = &limit

	return filter
}
