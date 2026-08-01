package dataprivacy

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/primandproper/platform-go/v9/database"
	"github.com/primandproper/platform-go/v9/database/dialect"
	"github.com/primandproper/platform-go/v9/dataprivacy/migrations"
	platformerrors "github.com/primandproper/platform-go/v9/errors"
	"github.com/primandproper/platform-go/v9/filtering"
)

// DefaultTablePrefix is the prefix the data privacy tables carry when no other
// is configured.
const DefaultTablePrefix = "dataprivacy"

var _ Store = (*sqlStore)(nil)

// sqlStore is the SQL-backed Store, against the schema dataprivacy/migrations
// renders.
type sqlStore struct {
	client  database.Client
	tables  *tables
	dialect dialect.Dialect
}

// SQLStoreOption configures a SQL Store.
type SQLStoreOption func(*sqlStore)

// WithTablePrefix overrides DefaultTablePrefix. It must be a plain SQL
// identifier fragment: it is interpolated into the query text, not bound as a
// parameter, and it must match the prefix the migrations were rendered with.
func WithTablePrefix(prefix string) SQLStoreOption {
	return func(s *sqlStore) {
		if prefix != "" {
			s.tables = newTables(prefix)
		}
	}
}

// NewSQLStore builds a Store over the given database.
//
// The dialect must match the client, and the prefix must match the one the
// migrations were rendered with — nothing here can check either, and a mismatch
// surfaces as a missing table on the first query rather than at construction.
func NewSQLStore(d dialect.Dialect, client database.Client, opts ...SQLStoreOption) (Store, error) {
	if !d.Valid() {
		return nil, platformerrors.Wrapf(dialect.ErrUnsupported, "dataprivacy dialect %q", d)
	}

	if client == nil {
		return nil, ErrNilDatabaseClient
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

	return s, nil
}

func (s *sqlStore) Save(ctx context.Context, q database.SQLQueryExecutor, req *Request) error {
	if q == nil {
		return ErrNilExecutor
	}

	if req == nil {
		return ErrNilRequest
	}

	failures, retained, err := encodeMaps(req)
	if err != nil {
		return err
	}

	query, args := s.tables.buildInsertRequest(s.dialect, req, failures, retained)

	if _, err = q.ExecContext(ctx, query, args...); err != nil {
		return platformerrors.Wrap(err, "inserting dataprivacy request")
	}

	return nil
}

func (s *sqlStore) Get(ctx context.Context, requestID string) (*Request, error) {
	query, args := s.tables.buildSelectRequest(s.dialect, requestID)

	req, err := scanRequest(s.client.Reader().QueryRowContext(ctx, query, args...))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, platformerrors.Wrapf(ErrRequestNotFound, "dataprivacy request %q", requestID)
		}

		return nil, platformerrors.Wrap(err, "reading dataprivacy request")
	}

	return req, nil
}

func (s *sqlStore) List(
	ctx context.Context,
	subject Subject,
	filter *filtering.QueryFilter,
) (*filtering.QueryFilteredResult[Request], error) {
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

	query, args := s.tables.buildListRequests(s.dialect, subject, cursor, limit, descending)

	requests, err := scanRequests(ctx, s.client.Reader(), query, args)
	if err != nil {
		return nil, platformerrors.Wrap(err, "listing dataprivacy requests")
	}

	countQuery, countArgs := s.tables.buildCountRequests(s.dialect, subject)

	var total uint64
	if err = s.client.Reader().QueryRowContext(ctx, countQuery, countArgs...).Scan(&total); err != nil {
		return nil, platformerrors.Wrap(err, "counting dataprivacy requests")
	}

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
	if q == nil {
		return nil, ErrNilExecutor
	}

	if len(from) == 0 {
		return nil, platformerrors.Wrap(platformerrors.ErrEmptyInputParameter, "no source statuses for dataprivacy transition")
	}

	query, args := s.tables.buildTransition(s.dialect, requestID, from, to, at)

	result, err := q.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, platformerrors.Wrap(err, "transitioning dataprivacy request")
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return nil, platformerrors.Wrap(err, "reading dataprivacy transition result")
	}

	if affected == 0 {
		return nil, platformerrors.Wrapf(ErrRequestNotFound, "dataprivacy request %q in expected status", requestID)
	}

	// Re-read through the same executor, so the caller sees the row as its own
	// transaction has it. Reading through the client here would go to the read
	// replica and could return the pre-transition row.
	selectQuery, selectArgs := s.tables.buildSelectRequest(s.dialect, requestID)

	req, err := scanRequest(q.QueryRowContext(ctx, selectQuery, selectArgs...))
	if err != nil {
		return nil, platformerrors.Wrap(err, "reading transitioned dataprivacy request")
	}

	return req, nil
}

func (s *sqlStore) Claim(ctx context.Context, now time.Time, limit int, leaseUntil time.Time) ([]*Request, error) {
	if limit <= 0 {
		return nil, nil
	}

	var claimed []*Request

	// The select and the update run in one transaction so that FOR UPDATE SKIP
	// LOCKED means anything. Without it the lock is released before the update,
	// and two workers select the same rows.
	err := s.client.WithTransaction(ctx, func(q database.SQLQueryExecutor) error {
		selectQuery, selectArgs := s.tables.buildSelectClaimable(s.dialect, now, limit, true)

		ids, err := scanIDs(ctx, q, selectQuery, selectArgs)
		if err != nil {
			return platformerrors.Wrap(err, "selecting claimable dataprivacy requests")
		}

		if len(ids) == 0 {
			return nil
		}

		claimQuery, claimArgs := s.tables.buildClaim(s.dialect, ids, leaseUntil)
		if _, err = q.ExecContext(ctx, claimQuery, claimArgs...); err != nil {
			return platformerrors.Wrap(err, "claiming dataprivacy requests")
		}

		// Re-read rather than project from the select, so the attempt counts the
		// worker sees are the ones the claim just wrote. A worker deciding
		// whether it has exhausted its budget from a pre-increment count would
		// grant every request one attempt more than configured.
		fetchQuery, fetchArgs := s.tables.buildFetchByIDs(s.dialect, ids, StatusProcessing)

		if claimed, err = scanRequests(ctx, q, fetchQuery, fetchArgs); err != nil {
			return platformerrors.Wrap(err, "reading claimed dataprivacy requests")
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return claimed, nil
}

func (s *sqlStore) CompleteExport(ctx context.Context, q database.SQLQueryExecutor, req *Request, at time.Time) error {
	if q == nil {
		return ErrNilExecutor
	}

	if req == nil {
		return ErrNilRequest
	}

	failures, err := encodeMap(req.Failures)
	if err != nil {
		return err
	}

	query, args := s.tables.buildCompleteExport(s.dialect, req, failures, at)

	return s.execExpectingRow(ctx, q, query, args, req.ID, "completing dataprivacy export")
}

func (s *sqlStore) WithTransaction(ctx context.Context, fn func(q database.SQLQueryExecutor) error) error {
	return s.client.WithTransaction(ctx, fn)
}

func (s *sqlStore) CompleteErasure(ctx context.Context, q database.SQLQueryExecutor, req *Request, at time.Time) error {
	if q == nil {
		return ErrNilExecutor
	}

	if req == nil {
		return ErrNilRequest
	}

	failures, retained, err := encodeMaps(req)
	if err != nil {
		return err
	}

	query, args := s.tables.buildCompleteErasure(s.dialect, req, failures, retained, at)

	return s.execExpectingRow(ctx, q, query, args, req.ID, "completing dataprivacy erasure")
}

func (s *sqlStore) Fail(
	ctx context.Context,
	requestID string,
	attempts int,
	nextAttempt time.Time,
	lastErr string,
	terminal bool,
) error {
	query, args := s.tables.buildFail(s.dialect, requestID, attempts, nextAttempt, lastErr, terminal)

	if _, err := s.client.Writer().ExecContext(ctx, query, args...); err != nil {
		return platformerrors.Wrap(err, "recording dataprivacy request failure")
	}

	return nil
}

func (s *sqlStore) ExpiringArtifacts(ctx context.Context, now time.Time, limit int) ([]*Request, error) {
	if limit <= 0 {
		return nil, nil
	}

	query, args := s.tables.buildSelectExpiringArtifacts(s.dialect, now, limit)

	requests, err := scanRequests(ctx, s.client.Reader(), query, args)
	if err != nil {
		return nil, platformerrors.Wrap(err, "selecting expiring dataprivacy artifacts")
	}

	return requests, nil
}

func (s *sqlStore) MarkExpired(ctx context.Context, requestID string, at time.Time) error {
	query, args := s.tables.buildMarkExpired(s.dialect, requestID, at)

	if _, err := s.client.Writer().ExecContext(ctx, query, args...); err != nil {
		return platformerrors.Wrap(err, "expiring dataprivacy artifact")
	}

	return nil
}

func (s *sqlStore) LapseUnconfirmed(ctx context.Context, now time.Time, limit int) (int64, error) {
	if limit <= 0 {
		return 0, nil
	}

	query, args := s.tables.buildLapseUnconfirmed(s.dialect, now, limit)

	result, err := s.client.Writer().ExecContext(ctx, query, args...)
	if err != nil {
		return 0, platformerrors.Wrap(err, "lapsing unconfirmed dataprivacy erasures")
	}

	lapsed, err := result.RowsAffected()
	if err != nil {
		return 0, platformerrors.Wrap(err, "reading lapsed dataprivacy erasure count")
	}

	return lapsed, nil
}

func (s *sqlStore) CountOverdue(ctx context.Context, now time.Time) (counts map[RequestType]int64, err error) {
	query, args := s.tables.buildCountOverdue(s.dialect, now)

	rows, err := s.client.Reader().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, platformerrors.Wrap(err, "counting overdue dataprivacy requests")
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
			return nil, platformerrors.Wrap(err, "scanning overdue dataprivacy request count")
		}

		counts[RequestType(requestType)] = count
	}

	if err = rows.Err(); err != nil {
		return nil, platformerrors.Wrap(err, "iterating overdue dataprivacy request counts")
	}

	return counts, nil
}

func (s *sqlStore) Reap(ctx context.Context, before time.Time, limit int) (int64, error) {
	if limit <= 0 {
		return 0, nil
	}

	query, args := s.tables.buildReap(s.dialect, before, limit)

	result, err := s.client.Writer().ExecContext(ctx, query, args...)
	if err != nil {
		return 0, platformerrors.Wrap(err, "reaping dataprivacy requests")
	}

	reaped, err := result.RowsAffected()
	if err != nil {
		return 0, platformerrors.Wrap(err, "reading reaped dataprivacy request count")
	}

	return reaped, nil
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
	q database.SQLQueryExecutor,
	query string,
	args []any,
	requestID, description string,
) error {
	result, err := q.ExecContext(ctx, query, args...)
	if err != nil {
		return platformerrors.Wrap(err, description)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return platformerrors.Wrapf(err, "reading result of %s", description)
	}

	if affected == 0 {
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
