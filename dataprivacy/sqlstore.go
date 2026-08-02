package dataprivacy

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/primandproper/platform-go/v9/database"
	"github.com/primandproper/platform-go/v9/database/dialect"
	"github.com/primandproper/platform-go/v9/dataprivacy/migrations"
	platformerrors "github.com/primandproper/platform-go/v9/errors"
	"github.com/primandproper/platform-go/v9/filtering"
	"github.com/primandproper/platform-go/v9/observability"
	"github.com/primandproper/platform-go/v9/observability/logging"
	"github.com/primandproper/platform-go/v9/observability/metrics"
	"github.com/primandproper/platform-go/v9/observability/tracing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// DefaultTablePrefix is the namespace the dataprivacy tables carry when none is
// configured, which is none — rendering dataprivacy_requests.
//
// The dataprivacy_ segment is the schema's, not the caller's: a table always says
// which package created it. Setting a namespace of "ddb" renders
// ddb_dataprivacy_requests, for a database shared between applications. A namespace must
// not end in '_'; database/ddl supplies the separator.
const DefaultTablePrefix = ""

// storeName scopes the store's spans and logger. It is deliberately not
// serviceName: a trace showing the state machine and the rows it moved wants
// those distinguishable, and one scope for both would make a store read look
// like a Service call in every span listing.
const storeName = serviceName + "_store"

var _ Store = (*sqlStore)(nil)

// sqlStore is the SQL-backed Store, against the schema dataprivacy/migrations
// renders.
type sqlStore struct {
	client database.Client
	tables *tables
	o11y   observability.Observer
	logger logging.Logger

	guardMissCounter metrics.Int64Counter

	tracerProvider  tracing.TracerProvider
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
		return nil, platformerrors.Wrapf(dialect.ErrUnsupported, "dataprivacy dialect %q", d)
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
	s.logger = s.o11y.Logger()

	// One counter, and only one. The Worker and Sweeper already own the business
	// totals — claimed, completed, failed, lapsed, reaped, expired — and a second
	// name for the same event is how two dashboards come to disagree. What no
	// caller can count is this: a guarded write that matched no row. That is not
	// a database error, and above this layer it is indistinguishable from one.
	mp := metrics.EnsureMetricsProvider(s.metricsProvider)

	var err error
	if s.guardMissCounter, err = mp.NewInt64Counter(storeName + "_guard_misses"); err != nil {
		return nil, platformerrors.Wrap(err, "creating dataprivacy store guard miss counter")
	}

	return s, nil
}

// storeOpAttr labels a guard miss with the operation that missed, so the metric
// distinguishes a subject confirming twice from a worker losing a completion
// race — one is routine and one wants looking at.
func storeOpAttr(operation string) metric.MeasurementOption {
	return metric.WithAttributes(attribute.String(storeOpKey, operation))
}

func (s *sqlStore) Save(ctx context.Context, q database.SQLQueryExecutor, req *Request) error {
	ctx, op := s.o11y.Begin(ctx)
	defer op.End()

	if q == nil {
		return op.Error(ErrNilExecutor, "saving dataprivacy request")
	}

	if req == nil {
		return op.Error(ErrNilRequest, "saving dataprivacy request")
	}

	op.SetValues(map[string]any{
		requestIDKey:   req.ID,
		requestTypeKey: string(req.Type),
		statusKey:      string(req.Status),
		subjectIDKey:   req.Subject.ID,
	})

	failures, retained, err := encodeMaps(req)
	if err != nil {
		return op.Error(err, "encoding dataprivacy request maps")
	}

	query, args := s.tables.buildInsertRequest(s.dialect, req, failures, retained)

	if _, err = q.ExecContext(ctx, query, args...); err != nil {
		return op.Error(err, "inserting dataprivacy request")
	}

	return nil
}

func (s *sqlStore) Get(ctx context.Context, requestID string) (*Request, error) {
	ctx, op := s.o11y.Begin(ctx, observability.WithValue(requestIDKey, requestID))
	defer op.End()

	query, args := s.tables.buildSelectRequest(s.dialect, requestID)

	req, err := scanRequest(s.client.Reader().QueryRowContext(ctx, query, args...))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Attached to the span but not logged as an error. A request ID that
			// is not in the table is a 404 somebody is owed, or a record
			// retention has swept — neither is a fault of this process, and
			// painting the trace red for it buries the ones that are.
			op.Set(guardMissedKey, true)

			return nil, platformerrors.Wrapf(ErrRequestNotFound, "dataprivacy request %q", requestID)
		}

		return nil, op.Error(err, "reading dataprivacy request")
	}

	op.Set(statusKey, string(req.Status)).Set(requestTypeKey, string(req.Type))

	return req, nil
}

func (s *sqlStore) List(
	ctx context.Context,
	subject Subject,
	filter *filtering.QueryFilter,
) (*filtering.QueryFilteredResult[Request], error) {
	ctx, op := s.o11y.Begin(ctx, observability.WithValues(map[string]any{
		subjectIDKey:    subject.ID,
		subjectTypeKey:  string(subject.Type),
		subjectScopeKey: subject.Scope,
	}))
	defer op.End()

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

	query, args := s.tables.buildListRequests(s.dialect, subject, cursor, limit, descending)

	requests, err := scanRequests(ctx, s.client.Reader(), query, args)
	if err != nil {
		return nil, op.Error(err, "listing dataprivacy requests")
	}

	countQuery, countArgs := s.tables.buildCountRequests(s.dialect, subject)

	var total uint64
	if err = s.client.Reader().QueryRowContext(ctx, countQuery, countArgs...).Scan(&total); err != nil {
		return nil, op.Error(err, "counting dataprivacy requests")
	}

	op.Set(resultCountKey, len(requests)).Set(resultTotalKey, total)

	return filtering.NewQueryFilteredResult(
		requests, uint64(len(requests)), total,
		func(r *Request) string { return r.ID },
		filter,
	), nil
}

func (s *sqlStore) Transition(
	ctx context.Context,
	q database.SQLQueryExecutor,
	requestID string,
	from []Status,
	to Status,
	at time.Time,
) (*Request, error) {
	ctx, op := s.o11y.Begin(ctx, observability.WithValues(map[string]any{
		requestIDKey:  requestID,
		statusKey:     string(to),
		fromStatusKey: statusStrings(from),
	}))
	defer op.End()

	if q == nil {
		return nil, op.Error(ErrNilExecutor, "transitioning dataprivacy request")
	}

	if len(from) == 0 {
		return nil, op.Error(platformerrors.ErrEmptyInputParameter, "no source statuses for dataprivacy transition")
	}

	query, args := s.tables.buildTransition(s.dialect, requestID, from, to, at)

	result, err := q.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, op.Error(err, "transitioning dataprivacy request")
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return nil, op.Error(err, "reading dataprivacy transition result")
	}

	op.Set(rowsAffectedKey, affected)

	if affected == 0 {
		// The guard in the predicate did its job and nothing moved: the request
		// is gone, or a concurrent writer got there first — a subject clicking
		// confirm twice, or the lapse sweep cancelling as they clicked. Counted
		// rather than logged as an error, because from here it is not
		// distinguishable from ordinary contention, and it is the caller that
		// knows whether losing this particular race matters.
		op.Set(guardMissedKey, true)
		s.guardMissCounter.Add(ctx, 1, storeOpAttr("transition"))

		return nil, platformerrors.Wrapf(ErrRequestNotFound, "dataprivacy request %q in expected status", requestID)
	}

	// Re-read through the same executor, so the caller sees the row as its own
	// transaction has it. Reading through the client here would go to the read
	// replica and could return the pre-transition row.
	selectQuery, selectArgs := s.tables.buildSelectRequest(s.dialect, requestID)

	req, err := scanRequest(q.QueryRowContext(ctx, selectQuery, selectArgs...))
	if err != nil {
		return nil, op.Error(err, "reading transitioned dataprivacy request")
	}

	return req, nil
}

func (s *sqlStore) Claim(ctx context.Context, now time.Time, limit int, leaseUntil time.Time) ([]*Request, error) {
	ctx, op := s.o11y.Begin(ctx, observability.WithValue(limitKey, limit))
	defer op.End()

	if limit <= 0 {
		return nil, nil
	}

	var (
		claimed  []*Request
		selected int
	)

	// The select and the update run in one transaction so that FOR UPDATE SKIP
	// LOCKED means anything. Without it the lock is released before the update,
	// and two workers select the same rows.
	err := s.client.WithTransaction(ctx, func(q database.SQLQueryExecutor) error {
		selectQuery, selectArgs := s.tables.buildSelectClaimable(s.dialect, now, limit, true)

		ids, err := scanIDs(ctx, q, selectQuery, selectArgs)
		if err != nil {
			return op.Error(err, "selecting claimable dataprivacy requests")
		}

		selected = len(ids)
		op.Set(selectedKey, selected)

		if len(ids) == 0 {
			return nil
		}

		claimQuery, claimArgs := s.tables.buildClaim(s.dialect, ids, now, leaseUntil)
		if _, err = q.ExecContext(ctx, claimQuery, claimArgs...); err != nil {
			return op.Error(err, "claiming dataprivacy requests")
		}

		// Re-read rather than project from the select, so the attempt counts the
		// worker sees are the ones the claim just wrote. A worker deciding
		// whether it has exhausted its budget from a pre-increment count would
		// grant every request one attempt more than configured.
		fetchQuery, fetchArgs := s.tables.buildFetchByIDs(s.dialect, ids, StatusProcessing)

		if claimed, err = scanRequests(ctx, q, fetchQuery, fetchArgs); err != nil {
			return op.Error(err, "reading claimed dataprivacy requests")
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	op.Set(claimedKey, len(claimed))

	// Selected as pending, gone by the time the guarded UPDATE ran. buildClaim
	// repeats the status guard for exactly this case, and until now it absorbed
	// the difference silently: the batch simply came back smaller than the one
	// that was selected, with nothing to say a subject had cancelled in between.
	if selected != len(claimed) {
		op.Logger().WithValues(map[string]any{
			selectedKey: selected,
			claimedKey:  len(claimed),
		}).Info("dataprivacy requests left the claimable set mid-claim")
	}

	return claimed, nil
}

func (s *sqlStore) CompleteExport(ctx context.Context, q database.SQLQueryExecutor, req *Request, at time.Time) error {
	ctx, op := s.o11y.Begin(ctx)
	defer op.End()

	if q == nil {
		return op.Error(ErrNilExecutor, "completing dataprivacy export")
	}

	if req == nil {
		return op.Error(ErrNilRequest, "completing dataprivacy export")
	}

	op.SetValues(map[string]any{
		requestIDKey:    req.ID,
		artifactRefKey:  req.ArtifactRef,
		artifactSizeKey: req.ArtifactBytes,
		failureCountKey: len(req.Failures),
	})

	failures, err := encodeMap(req.Failures)
	if err != nil {
		return op.Error(err, "encoding dataprivacy export failures")
	}

	query, args := s.tables.buildCompleteExport(s.dialect, req, failures, at)

	return s.execExpectingRow(ctx, op, q, query, args, req.ID, "export", "completing dataprivacy export")
}

// WithTransaction delegates to the client, which begins its own span for the
// transaction. Wrapping it here would nest a second span around the first and
// say nothing the client's does not.
func (s *sqlStore) WithTransaction(ctx context.Context, fn func(q database.SQLQueryExecutor) error) error {
	return s.client.WithTransaction(ctx, fn)
}

func (s *sqlStore) CompleteErasure(ctx context.Context, q database.SQLQueryExecutor, req *Request, at time.Time) error {
	ctx, op := s.o11y.Begin(ctx)
	defer op.End()

	if q == nil {
		return op.Error(ErrNilExecutor, "completing dataprivacy erasure")
	}

	if req == nil {
		return op.Error(ErrNilRequest, "completing dataprivacy erasure")
	}

	op.SetValues(map[string]any{
		requestIDKey:    req.ID,
		deletedKey:      req.Deleted,
		anonymizedKey:   req.Anonymized,
		retainedKey:     len(req.Retained),
		failureCountKey: len(req.Failures),
	})

	failures, retained, err := encodeMaps(req)
	if err != nil {
		return op.Error(err, "encoding dataprivacy erasure maps")
	}

	query, args := s.tables.buildCompleteErasure(s.dialect, req, failures, retained, at)

	return s.execExpectingRow(ctx, op, q, query, args, req.ID, "erasure", "completing dataprivacy erasure")
}

func (s *sqlStore) Fail(
	ctx context.Context,
	requestID string,
	attempts int,
	nextAttempt time.Time,
	lastErr string,
	terminal bool,
) error {
	ctx, op := s.o11y.Begin(ctx, observability.WithValues(map[string]any{
		requestIDKey: requestID,
		attemptsKey:  attempts,
		terminalKey:  terminal,
	}))
	defer op.End()

	query, args := s.tables.buildFail(s.dialect, requestID, attempts, nextAttempt, lastErr, terminal)

	if _, err := s.client.Writer().ExecContext(ctx, query, args...); err != nil {
		return op.Error(err, "recording dataprivacy request failure")
	}

	return nil
}

func (s *sqlStore) ExpiringArtifacts(ctx context.Context, now time.Time, limit int) ([]*Request, error) {
	ctx, op := s.o11y.Begin(ctx, observability.WithValue(limitKey, limit))
	defer op.End()

	if limit <= 0 {
		return nil, nil
	}

	query, args := s.tables.buildSelectExpiringArtifacts(s.dialect, now, limit)

	requests, err := scanRequests(ctx, s.client.Reader(), query, args)
	if err != nil {
		return nil, op.Error(err, "selecting expiring dataprivacy artifacts")
	}

	op.Set(resultCountKey, len(requests))

	// A sweep that keeps coming back full is a sweep that is not keeping up, and
	// the thing it is failing to delete is a file containing everything the
	// application knows about somebody.
	if len(requests) == limit {
		op.Logger().WithValue(limitKey, limit).
			Info("dataprivacy artifact expiry sweep filled its batch; artifacts may be expiring faster than they are swept")
	}

	return requests, nil
}

func (s *sqlStore) MarkExpired(ctx context.Context, requestID string, at time.Time) error {
	ctx, op := s.o11y.Begin(ctx, observability.WithValue(requestIDKey, requestID))
	defer op.End()

	query, args := s.tables.buildMarkExpired(s.dialect, requestID, at)

	if _, err := s.client.Writer().ExecContext(ctx, query, args...); err != nil {
		return op.Error(err, "expiring dataprivacy artifact")
	}

	return nil
}

func (s *sqlStore) LapseUnconfirmed(ctx context.Context, now time.Time, limit int) (int64, error) {
	ctx, op := s.o11y.Begin(ctx, observability.WithValue(limitKey, limit))
	defer op.End()

	if limit <= 0 {
		return 0, nil
	}

	query, args := s.tables.buildLapseUnconfirmed(s.dialect, now, limit)

	result, err := s.client.Writer().ExecContext(ctx, query, args...)
	if err != nil {
		return 0, op.Error(err, "lapsing unconfirmed dataprivacy erasures")
	}

	lapsed, err := result.RowsAffected()
	if err != nil {
		return 0, op.Error(err, "reading lapsed dataprivacy erasure count")
	}

	op.Set(lapsedKey, lapsed)

	return lapsed, nil
}

func (s *sqlStore) CountOverdue(ctx context.Context, now time.Time) (counts map[RequestType]int64, err error) {
	ctx, op := s.o11y.Begin(ctx)
	defer op.End()

	query, args := s.tables.buildCountOverdue(s.dialect, now)

	rows, err := s.client.Reader().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, op.Error(err, "counting overdue dataprivacy requests")
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = platformerrors.Wrap(closeErr, "closing dataprivacy overdue count rows")
		}
	}()

	// Seeded with a zero for every type, so a gauge that was reporting three
	// overdue exports actively drops to zero when they are served rather than
	// holding a stale reading on the dashboard forever.
	counts = map[RequestType]int64{RequestExport: 0, RequestErasure: 0}

	for rows.Next() {
		var (
			requestType string
			count       int64
		)

		if err = rows.Scan(&requestType, &count); err != nil {
			return nil, op.Error(err, "scanning overdue dataprivacy request count")
		}

		counts[RequestType(requestType)] = count
	}

	if err = rows.Err(); err != nil {
		return nil, op.Error(err, "iterating overdue dataprivacy request counts")
	}

	op.Set(overdueKey, counts[RequestExport]+counts[RequestErasure])

	return counts, nil
}

func (s *sqlStore) Reap(ctx context.Context, before time.Time, limit int) (int64, error) {
	ctx, op := s.o11y.Begin(ctx, observability.WithValue(limitKey, limit))
	defer op.End()

	if limit <= 0 {
		return 0, nil
	}

	query, args := s.tables.buildReap(s.dialect, before, limit)

	result, err := s.client.Writer().ExecContext(ctx, query, args...)
	if err != nil {
		return 0, op.Error(err, "reaping dataprivacy requests")
	}

	reaped, err := result.RowsAffected()
	if err != nil {
		return 0, op.Error(err, "reading reaped dataprivacy request count")
	}

	op.Set(reapedKey, reaped)

	return reaped, nil
}

// statusStrings renders a status set for a span attribute. Spans take scalars
// and strings, not []Status, and the set a transition guarded on is the first
// thing wanted when one of them matches nothing.
func statusStrings(statuses []Status) string {
	rendered := make([]string, 0, len(statuses))
	for _, status := range statuses {
		rendered = append(rendered, string(status))
	}

	return strings.Join(rendered, ",")
}

// execExpectingRow runs a guarded UPDATE and reports a request that was not in
// the status the guard required.
//
// The distinction matters more here than it looks. A completion that matches no
// rows means the request left StatusProcessing while the worker was busy —
// cancelled, or expired, or claimed by a second worker after a lease lapsed —
// and treating that as success would have the worker report an export delivered
// against a row that says otherwise.
func (s *sqlStore) execExpectingRow(
	ctx context.Context,
	op observability.Operation,
	q database.SQLQueryExecutor,
	query string,
	args []any,
	requestID, operation, description string,
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
		// The work is done — the artifact is uploaded, or the rows are deleted —
		// and the row that should record it has moved on without us. Everything
		// needed to find the request by hand goes in the line.
		op.Set(guardMissedKey, true)
		s.guardMissCounter.Add(ctx, 1, storeOpAttr(operation))
		op.Logger().WithValue(requestIDKey, requestID).
			Info("dataprivacy request left processing before its completion could be recorded")

		return platformerrors.Wrapf(ErrRequestNotFound, "dataprivacy request %q is no longer being processed", requestID)
	}

	return nil
}

// scanIDs drains a single-column ID projection.
func scanIDs(ctx context.Context, q database.SQLQueryExecutor, query string, args []any) (ids []string, err error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = platformerrors.Wrap(closeErr, "closing dataprivacy request ID rows")
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

// scanRequests drains a request projection.
func scanRequests(ctx context.Context, q database.SQLQueryExecutor, query string, args []any) (requests []*Request, err error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = platformerrors.Wrap(closeErr, "closing dataprivacy request rows")
		}
	}()

	for rows.Next() {
		req, scanErr := scanRequest(rows)
		if scanErr != nil {
			return nil, scanErr
		}

		requests = append(requests, req)
	}

	return requests, rows.Err()
}

// scanRequest reads one row of requestColumns.
func scanRequest(scanner database.Scanner) (*Request, error) {
	var (
		req         Request
		requestType string
		status      string
		subjectType string
		expiresAt   sql.NullTime
		completedAt sql.NullTime
		failures    []byte
		retained    []byte
		lastError   sql.NullString
	)

	if err := scanner.Scan(
		&req.ID, &requestType, &status,
		&req.Subject.ID, &subjectType, &req.Subject.Scope,
		&req.RequestedAt, &req.DueAt, &expiresAt, &completedAt, &req.Attempts,
		&req.ArtifactRef, &req.ArtifactBytes, &req.Deleted, &req.Anonymized,
		&failures, &retained, &lastError,
	); err != nil {
		return nil, err
	}

	req.Type = RequestType(requestType)
	req.Status = Status(status)
	req.Subject.Type = SubjectType(subjectType)
	req.RequestedAt = req.RequestedAt.UTC()
	req.DueAt = req.DueAt.UTC()
	req.ExpiresAt = database.TimeFromNullTime(expiresAt).UTC()
	req.CompletedAt = database.TimePointerFromNullTime(completedAt)
	req.LastError = database.StringFromNullString(lastError)

	if req.CompletedAt != nil {
		utc := req.CompletedAt.UTC()
		req.CompletedAt = &utc
	}

	var err error
	if req.Failures, err = decodeMap(failures); err != nil {
		return nil, platformerrors.Wrap(err, "decoding dataprivacy request failures")
	}

	if req.Retained, err = decodeMap(retained); err != nil {
		return nil, platformerrors.Wrap(err, "decoding dataprivacy request retentions")
	}

	return &req, nil
}

// encodeMaps renders both of a request's string maps for storage.
func encodeMaps(req *Request) (failures, retained []byte, err error) {
	if failures, err = encodeMap(req.Failures); err != nil {
		return nil, nil, err
	}

	if retained, err = encodeMap(req.Retained); err != nil {
		return nil, nil, err
	}

	return failures, retained, nil
}

// encodeMap renders a string map for storage, or nil for an empty one. Nil and
// empty collapse deliberately: they say the same thing, and storing two
// renderings would make a round trip depend on which call site wrote the row.
func encodeMap(m map[string]string) ([]byte, error) {
	if len(m) == 0 {
		return nil, nil
	}

	encoded, err := json.Marshal(m)
	if err != nil {
		return nil, platformerrors.Wrap(err, "encoding dataprivacy request map")
	}

	return encoded, nil
}

// decodeMap reads a stored string map back, leaving an absent one nil.
//
// A nil map with a nil error is the intended result for a NULL column, not a
// missing value: "no failures" and "no retentions" are the common case, and a
// sentinel here would make every read branch on an error that means nothing
// went wrong.
func decodeMap(b []byte) (m map[string]string, err error) {
	if len(b) == 0 {
		return nil, nil //nolint:nilnil // an absent map is the normal reading, not an error
	}

	if err = json.Unmarshal(b, &m); err != nil {
		return nil, err
	}

	return m, nil
}
