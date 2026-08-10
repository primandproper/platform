package saga

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/primandproper/platform-go/v10/database"
	"github.com/primandproper/platform-go/v10/database/dialect"
	platformerrors "github.com/primandproper/platform-go/v10/errors"
	"github.com/primandproper/platform-go/v10/filtering"
	"github.com/primandproper/platform-go/v10/observability"
	"github.com/primandproper/platform-go/v10/observability/logging"
	"github.com/primandproper/platform-go/v10/observability/metrics"
	"github.com/primandproper/platform-go/v10/observability/tracing"
	"github.com/primandproper/platform-go/v10/saga/migrations"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// DefaultTablePrefix is the namespace the saga tables carry when none is
// configured, which is none — rendering saga_instances.
//
// The saga_ segment is the schema's, not the caller's: a table always says
// which package created it. Setting a namespace of "ddb" renders
// ddb_saga_instances, for a database shared between applications. A namespace must
// not end in '_'; database/ddl supplies the separator.
const DefaultTablePrefix = ""

// storeName scopes the store's spans and logger. It is deliberately not
// serviceName: a trace showing a saga advancing and the rows it moved wants
// those distinguishable, and one scope for both would make a store read look
// like a step execution in every span listing.
const storeName = serviceName + "_store"

var _ Store = (*sqlStore)(nil)

// sqlStore is the SQL-backed Store, against the schema saga/migrations renders.
type sqlStore struct {
	client database.Client
	tables *tables
	o11y   observability.Observer

	guardMissCounter metrics.Int64Counter

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
// to a noop logger, traces to a noop provider, and counts into a noop meter.
func NewSQLStore(client database.Client, opts ...SQLStoreOption) (Store, error) {
	if client == nil {
		return nil, ErrNilDatabaseClient
	}

	d := client.Dialect()
	if !d.Valid() {
		return nil, platformerrors.Wrapf(dialect.ErrUnsupported, "saga dialect %q", d)
	}

	s := &sqlStore{
		client:  client,
		dialect: d,
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

	// One counter, and only one. The Worker owns the business totals — started,
	// advanced, compensated, stuck — and a second name for the same event is how
	// two dashboards come to disagree. What no caller can count is this: a
	// guarded write that matched no row. That is not a database error, and above
	// this layer it is indistinguishable from one.
	mp := metrics.EnsureMetricsProvider(s.metricsProvider)

	var err error
	if s.guardMissCounter, err = mp.NewInt64Counter(storeName + "_guard_misses"); err != nil {
		return nil, platformerrors.Wrap(err, "creating saga store guard miss counter")
	}

	return s, nil
}

// storeOpAttr labels a guard miss with the operation that missed, so the metric
// distinguishes an operator resuming twice from a worker losing an advance race
// — one is routine and one wants looking at.
func storeOpAttr(operation string) metric.MeasurementOption {
	return metric.WithAttributes(attribute.String(storeOpKey, operation))
}

func (s *sqlStore) Save(ctx context.Context, q database.SQLQueryExecutor, inst *Record, nextAttempt time.Time) error {
	ctx, op := s.o11y.Begin(ctx)
	defer op.End()

	if q == nil {
		return op.Error(ErrNilExecutor, "saving saga instance")
	}

	if inst == nil {
		return op.Error(ErrNilInstance, "saving saga instance")
	}

	op.SetValues(map[string]any{
		instanceIDKey:  inst.ID,
		definitionKey:  inst.Definition,
		statusKey:      string(inst.Status),
		stepCountKey:   len(inst.StepNames),
		nextAttemptKey: nextAttempt,
	})

	query, args := s.tables.buildInsertInstance(s.dialect, inst, encodeStepNames(inst.StepNames), nextAttempt)

	if _, err := q.ExecContext(ctx, query, args...); err != nil {
		return op.Error(err, "inserting saga instance")
	}

	return nil
}

// encodeStepNames renders a step list for storage.
//
// It returns no error because it cannot produce one. json.Marshal fails on
// cycles, channels, funcs, and NaNs; a []string is none of those, and an error
// branch here would be one nothing can reach and no test can cover. The
// decode side does return an error, because a column can be edited by hand.
func encodeStepNames(names []string) []byte {
	//nolint:errcheck,errchkjson // a []string always marshals; see the comment above.
	encoded, _ := json.Marshal(names)

	return encoded
}

func (s *sqlStore) Get(ctx context.Context, instanceID string) (*Record, error) {
	ctx, op := s.o11y.Begin(ctx, observability.WithValue(instanceIDKey, instanceID))
	defer op.End()

	query, args := s.tables.buildSelectInstance(s.dialect, instanceID)

	inst, err := scanInstance(s.client.Reader().QueryRowContext(ctx, query, args...))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Attached to the span but not logged as an error. An instance ID
			// that is not in the table is a 404 somebody is owed, not a fault of
			// this process, and painting the trace red for it buries the ones
			// that are.
			op.Set(guardMissedKey, true)

			return nil, platformerrors.Wrapf(ErrInstanceNotFound, "saga instance %q", instanceID)
		}

		return nil, op.Error(err, "reading saga instance")
	}

	op.Set(statusKey, string(inst.Status)).Set(definitionKey, inst.Definition)

	return inst, nil
}

func (s *sqlStore) List(
	ctx context.Context,
	scope *ListScope,
	filter *filtering.QueryFilter,
) (*filtering.QueryFilteredResult[Record], error) {
	ctx, op := s.o11y.Begin(ctx)
	defer op.End()

	if scope != nil {
		op.Set(definitionKey, scope.Definition).Set(statusKey, statusStrings(scope.Statuses))
	}

	if filter == nil {
		filter = filtering.DefaultQueryFilter()
	}

	limit := int(filtering.DefaultQueryFilterLimit)
	if filter.MaxResponseSize != nil && *filter.MaxResponseSize > 0 {
		limit = int(*filter.MaxResponseSize)
	}

	var cursor string
	if filter.Cursor != nil {
		cursor = *filter.Cursor
	}

	// Ordering follows the filter rather than a package-local preference.
	// filtering.DefaultQueryFilter asks for ascending, and a package that
	// quietly reversed it would make this the one list endpoint in the module
	// whose sort does not mean what the shared filter says it means.
	descending := filter.SortBy != nil && *filter.SortBy == *filtering.SortDescending

	op.Set(limitKey, limit)

	query, args := s.tables.buildListInstances(s.dialect, scope, cursor, limit, descending)

	instances, err := scanInstances(ctx, s.client.Reader(), query, args)
	if err != nil {
		return nil, op.Error(err, "listing saga instances")
	}

	countQuery, countArgs := s.tables.buildCountInstances(s.dialect, scope)

	var total uint64
	if err = s.client.Reader().QueryRowContext(ctx, countQuery, countArgs...).Scan(&total); err != nil {
		return nil, op.Error(err, "counting saga instances")
	}

	op.Set(resultCountKey, len(instances)).Set(resultTotalKey, total)

	return filtering.NewQueryFilteredResult(
		instances, uint64(len(instances)), total,
		func(r *Record) string { return r.ID },
		filter,
	), nil
}

func (s *sqlStore) Claim(ctx context.Context, now time.Time, limit int, leaseUntil time.Time) ([]*Record, error) {
	ctx, op := s.o11y.Begin(ctx, observability.WithValue(limitKey, limit))
	defer op.End()

	if limit <= 0 {
		return nil, nil
	}

	var (
		claimed  []*Record
		selected int
	)

	// The select and the update run in one transaction so that FOR UPDATE SKIP
	// LOCKED means anything. Without it the lock is released before the update,
	// and two workers select the same rows.
	err := s.client.WithTransaction(ctx, func(q database.SQLQueryExecutor) error {
		selectQuery, selectArgs := s.tables.buildSelectClaimable(s.dialect, now, limit, true)

		ids, err := scanIDs(ctx, q, selectQuery, selectArgs)
		if err != nil {
			return op.Error(err, "selecting claimable saga instances")
		}

		selected = len(ids)
		op.Set(selectedKey, selected)

		if len(ids) == 0 {
			return nil
		}

		claimQuery, claimArgs := s.tables.buildClaim(s.dialect, ids, leaseUntil, now)
		if _, err = q.ExecContext(ctx, claimQuery, claimArgs...); err != nil {
			return op.Error(err, "claiming saga instances")
		}

		// Re-read rather than project from the select, so the attempt counts the
		// worker sees are the ones the claim just wrote. A worker deciding
		// whether a step has exhausted its budget from a pre-increment count
		// would grant every step one attempt more than configured.
		fetchQuery, fetchArgs := s.tables.buildFetchByIDs(s.dialect, ids)

		if claimed, err = scanInstances(ctx, q, fetchQuery, fetchArgs); err != nil {
			return op.Error(err, "reading claimed saga instances")
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	op.Set(claimedKey, len(claimed))

	// Selected as active, gone by the time the guarded UPDATE ran: another
	// worker's advance finished the saga in between. buildClaim repeats the
	// status guard for exactly this case, and without this line the batch would
	// simply come back smaller with nothing to say why.
	if selected != len(claimed) {
		op.Logger().WithValues(map[string]any{
			selectedKey: selected,
			claimedKey:  len(claimed),
		}).Info("saga instances left the claimable set mid-claim")
	}

	return claimed, nil
}

func (s *sqlStore) Advance(ctx context.Context, q database.SQLQueryExecutor, inst *Record, nextAttempt time.Time) error {
	ctx, op := s.o11y.Begin(ctx)
	defer op.End()

	if q == nil {
		return op.Error(ErrNilExecutor, "advancing saga instance")
	}

	if inst == nil {
		return op.Error(ErrNilInstance, "advancing saga instance")
	}

	op.SetValues(map[string]any{
		instanceIDKey:  inst.ID,
		statusKey:      string(inst.Status),
		stepIndexKey:   inst.CurrentStep,
		stateBytesKey:  len(inst.State),
		nextAttemptKey: nextAttempt,
	})

	query, args := s.tables.buildAdvance(s.dialect, inst, nextAttempt, inst.UpdatedAt)

	return s.execExpectingRow(ctx, op, q, query, args, inst.ID, "advance", "advancing saga instance")
}

func (s *sqlStore) Reschedule(
	ctx context.Context,
	instanceID string,
	attempts int,
	nextAttempt time.Time,
	lastErr string,
	at time.Time,
) error {
	ctx, op := s.o11y.Begin(ctx, observability.WithValues(map[string]any{
		instanceIDKey:  instanceID,
		attemptsKey:    attempts,
		nextAttemptKey: nextAttempt,
	}))
	defer op.End()

	query, args := s.tables.buildReschedule(s.dialect, instanceID, attempts, nextAttempt, lastErr, at)

	return s.execExpectingRow(ctx, op, s.client.Writer(), query, args, instanceID, "reschedule", "rescheduling saga instance")
}

func (s *sqlStore) Release(ctx context.Context, instanceID string, at time.Time) error {
	ctx, op := s.o11y.Begin(ctx, observability.WithValue(instanceIDKey, instanceID))
	defer op.End()

	query, args := s.tables.buildRelease(s.dialect, instanceID, at)

	if _, err := s.client.Writer().ExecContext(ctx, query, args...); err != nil {
		return op.Error(err, "releasing saga instance lease")
	}

	return nil
}

func (s *sqlStore) Requeue(
	ctx context.Context,
	instanceID string,
	from []Status,
	to Status,
	at time.Time,
) (*Record, error) {
	ctx, op := s.o11y.Begin(ctx, observability.WithValues(map[string]any{
		instanceIDKey: instanceID,
		statusKey:     string(to),
		fromStatusKey: statusStrings(from),
	}))
	defer op.End()

	if len(from) == 0 {
		return nil, op.Error(platformerrors.ErrEmptyInputParameter, "no source statuses for saga requeue")
	}

	query, args := s.tables.buildRequeue(s.dialect, instanceID, from, to, at)

	result, err := s.client.Writer().ExecContext(ctx, query, args...)
	if err != nil {
		return nil, op.Error(err, "requeuing saga instance")
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return nil, op.Error(err, "reading saga requeue result")
	}

	op.Set(rowsAffectedKey, affected)

	if affected == 0 {
		// The guard in the predicate did its job and nothing moved: the instance
		// is gone, or somebody resumed it a moment ago. Counted rather than
		// logged as an error, because from here the two are indistinguishable
		// and it is the caller that knows whether losing this race matters.
		op.Set(guardMissedKey, true)
		s.guardMissCounter.Add(ctx, 1, storeOpAttr("requeue"))

		return nil, platformerrors.Wrapf(ErrInstanceNotFound, "saga instance %q in expected status", instanceID)
	}

	return s.Get(ctx, instanceID)
}

// WithTransaction delegates to the client, which begins its own span for the
// transaction. Wrapping it here would nest a second span around the first and
// say nothing the client's does not.
func (s *sqlStore) WithTransaction(ctx context.Context, fn func(q database.SQLQueryExecutor) error) error {
	return s.client.WithTransaction(ctx, fn)
}

// execExpectingRow runs a guarded UPDATE and reports one that matched nothing.
//
// The distinction matters more here than it looks. An advance that matches no
// rows means the instance left the active set while the worker was mid-step —
// finished by another worker after a lease lapsed, or marked stuck by an
// operator — and treating that as success would have the worker carry on
// running steps against a saga the database says is over.
func (s *sqlStore) execExpectingRow(
	ctx context.Context,
	op observability.Operation,
	q database.SQLQueryExecutor,
	query string,
	args []any,
	instanceID, operation, description string,
) error {
	result, err := q.ExecContext(ctx, query, args...)
	if err != nil {
		return op.Error(err, "%s", description)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return op.Error(err, "reading result of %s", description)
	}

	op.Set(rowsAffectedKey, affected)

	if affected == 0 {
		// Logged rather than merely counted, because this one is worth reading.
		// The step already ran — the charge is posted, the object is written —
		// and the row that should record it has moved on without us. Everything
		// needed to find the instance by hand goes in the line.
		op.Set(guardMissedKey, true)
		s.guardMissCounter.Add(ctx, 1, storeOpAttr(operation))
		op.Logger().WithValue(instanceIDKey, instanceID).
			Info("saga instance left the active set before its progress could be recorded")

		return platformerrors.Wrapf(ErrInstanceNotFound, "saga instance %q is no longer advanceable", instanceID)
	}

	return nil
}

// statusStrings renders a status set for a span attribute. Spans take scalars
// and strings, not []Status, and the set a write guarded on is the first thing
// wanted when one of them matches nothing.
func statusStrings(statuses []Status) string {
	rendered := make([]string, 0, len(statuses))
	for _, status := range statuses {
		rendered = append(rendered, string(status))
	}

	return strings.Join(rendered, ",")
}

// scanIDs drains a single-column ID projection.
func scanIDs(ctx context.Context, q database.SQLQueryExecutor, query string, args []any) (ids []string, err error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = platformerrors.Wrap(closeErr, "closing saga instance ID rows")
		}
	}()

	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}

		ids = append(ids, id)
	}

	return ids, rows.Err()
}

// scanInstances drains an instance projection.
func scanInstances(ctx context.Context, q database.SQLQueryExecutor, query string, args []any) (instances []*Record, err error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = platformerrors.Wrap(closeErr, "closing saga instance rows")
		}
	}()

	for rows.Next() {
		inst, scanErr := scanInstance(rows)
		if scanErr != nil {
			return nil, scanErr
		}

		instances = append(instances, inst)
	}

	return instances, rows.Err()
}

// scanInstance reads one row of instanceColumns.
func scanInstance(scanner database.Scanner) (*Record, error) {
	var (
		inst         Record
		status       string
		resumeStatus string
		stepNames    string
		state        []byte
		lastError    sql.NullString
	)

	if err := scanner.Scan(
		&inst.ID, &inst.Definition, &status, &inst.CurrentStep, &stepNames, &state,
		&inst.Attempts, &lastError, &resumeStatus, &inst.StartedAt, &inst.UpdatedAt,
	); err != nil {
		return nil, err
	}

	inst.Status = Status(status)
	inst.ResumeStatus = Status(resumeStatus)
	inst.LastError = database.StringFromNullString(lastError)
	inst.StartedAt = inst.StartedAt.UTC()
	inst.UpdatedAt = inst.UpdatedAt.UTC()

	if len(state) > 0 {
		// Copied out of the driver's buffer. database/sql reuses the byte slice
		// backing a []byte destination across Next calls, so a claimed batch
		// would otherwise come back with every instance holding the last row's
		// state.
		inst.State = json.RawMessage(append([]byte(nil), state...))
	}

	if err := json.Unmarshal([]byte(stepNames), &inst.StepNames); err != nil {
		return nil, platformerrors.Wrap(err, "decoding saga step names")
	}

	return &inst, nil
}
