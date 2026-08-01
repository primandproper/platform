package webhooks

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/primandproper/platform-go/v9/database"
	"github.com/primandproper/platform-go/v9/database/dialect"
	platformerrors "github.com/primandproper/platform-go/v9/errors"
	"github.com/primandproper/platform-go/v9/filtering"
	"github.com/primandproper/platform-go/v9/identifiers"
	"github.com/primandproper/platform-go/v9/webhooks/migrations"
)

// DefaultTablePrefix is the namespace the webhooks tables carry when none is
// configured, which is none — rendering webhooks_endpoints and its four siblings.
//
// The webhooks_ segment is the schema's, not the caller's: a table always says
// which package created it. Setting a namespace of "ddb" renders
// ddb_webhooks_endpoints, for a database shared between applications. A namespace must
// not end in '_'; database/ddl supplies the separator.
const DefaultTablePrefix = ""

var _ Store = (*sqlStore)(nil)

// sqlStore is the SQL-backed Store, against the schema webhooks/migrations
// renders.
type sqlStore struct {
	client  database.Client
	tables  *tables
	dialect dialect.Dialect
}

// NewSQLStore builds a Store over the given database.
//
// The dialect comes from the client, so the two cannot disagree. The prefix must
// still match the one the migrations were rendered with — nothing here can check
// that, and a mismatch surfaces as a missing table on the first query rather
// than at construction.
func NewSQLStore(client database.Client, opts ...SQLStoreOption) (Store, error) {
	if client == nil {
		return nil, ErrNilDatabaseClient
	}

	d := client.Dialect()
	if !d.Valid() {
		return nil, platformerrors.Wrapf(dialect.ErrUnsupported, "webhooks dialect %q", d)
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

// ErrNilDatabaseClient indicates a nil database.Client. It wraps
// errors.ErrNilInputParameter, so a caller may check either.
var ErrNilDatabaseClient = platformerrors.Wrap(platformerrors.ErrNilInputParameter, "nil database client")

// SaveEndpoint upserts the endpoint and replaces its subscription set, both in
// one transaction — a half-registered endpoint would either receive events it
// no longer subscribes to or silently receive none.
func (s *sqlStore) SaveEndpoint(ctx context.Context, endpoint *Endpoint) error {
	if endpoint == nil {
		return ErrNilEndpoint
	}

	headers, err := json.Marshal(endpoint.Headers)
	if err != nil {
		return platformerrors.Wrap(err, "marshaling webhook endpoint headers")
	}

	now := time.Now().UTC()

	return s.client.WithTransaction(ctx, func(q database.SQLQueryExecutor) error {
		query, args := s.tables.buildUpsertEndpoint(s.dialect, endpoint, headers, now)
		if _, err = q.ExecContext(ctx, query, args...); err != nil {
			return platformerrors.Wrap(err, "upserting webhook endpoint")
		}

		query, args = s.tables.buildDeleteSubscriptions(s.dialect, endpoint.ID)
		if _, err = q.ExecContext(ctx, query, args...); err != nil {
			return platformerrors.Wrap(err, "clearing webhook endpoint subscriptions")
		}

		if len(endpoint.Events) == 0 {
			return nil
		}

		query, args = s.tables.buildInsertSubscriptions(s.dialect, endpoint.ID, endpoint.Events)
		if _, err = q.ExecContext(ctx, query, args...); err != nil {
			return platformerrors.Wrap(err, "inserting webhook endpoint subscriptions")
		}

		return nil
	})
}

// GetEndpoint reads one endpoint and its subscriptions.
func (s *sqlStore) GetEndpoint(ctx context.Context, endpointID string) (*Endpoint, error) {
	query, args := s.tables.buildSelectEndpoint(s.dialect, endpointID)

	endpoint, err := scanEndpoint(s.client.Reader().QueryRowContext(ctx, query, args...))
	if err != nil {
		return nil, platformerrors.Wrapf(err, "reading webhook endpoint %q", endpointID)
	}

	if endpoint.Events, err = s.subscriptionsFor(ctx, s.client.Reader(), endpointID); err != nil {
		return nil, err
	}

	return endpoint, nil
}

// ListEndpoints pages the registry.
func (s *sqlStore) ListEndpoints(ctx context.Context, filter *filtering.QueryFilter) (*filtering.QueryFilteredResult[Endpoint], error) {
	if filter == nil {
		filter = filtering.DefaultQueryFilter()
	}

	limit := filtering.DefaultQueryFilterLimit
	if filter.MaxResponseSize != nil && *filter.MaxResponseSize > 0 {
		limit = int(*filter.MaxResponseSize)
	}

	var cursor string
	if filter.Cursor != nil {
		cursor = *filter.Cursor
	}

	query, args := s.tables.buildListEndpoints(s.dialect, cursor, limit)

	endpoints, err := s.scanEndpoints(ctx, s.client.Reader(), query, args)
	if err != nil {
		return nil, platformerrors.Wrap(err, "listing webhook endpoints")
	}

	// Subscriptions are read per endpoint rather than through a join, so that
	// one endpoint with thirty event types does not multiply every other row in
	// the page by its subscription count.
	for _, endpoint := range endpoints {
		if endpoint.Events, err = s.subscriptionsFor(ctx, s.client.Reader(), endpoint.ID); err != nil {
			return nil, err
		}
	}

	var total uint64
	if err = s.client.Reader().QueryRowContext(ctx, s.tables.buildCountEndpoints()).Scan(&total); err != nil {
		return nil, platformerrors.Wrap(err, "counting webhook endpoints")
	}

	return filtering.NewQueryFilteredResult(
		endpoints, uint64(len(endpoints)), total,
		func(e *Endpoint) string { return e.ID },
		filter,
	), nil
}

// ArchiveEndpoint retires an endpoint.
func (s *sqlStore) ArchiveEndpoint(ctx context.Context, endpointID string) error {
	query, args := s.tables.buildArchiveEndpoint(s.dialect, endpointID, time.Now().UTC())

	if _, err := s.client.Writer().ExecContext(ctx, query, args...); err != nil {
		return platformerrors.Wrapf(err, "archiving webhook endpoint %q", endpointID)
	}

	return nil
}

// EndpointsForEvent resolves the fan-out set, using the caller's executor so it
// sees the same snapshot as the transaction that is dispatching.
func (s *sqlStore) EndpointsForEvent(ctx context.Context, q database.SQLQueryExecutor, eventType string) ([]*Endpoint, error) {
	if q == nil {
		return nil, ErrNilExecutor
	}

	query, args := s.tables.buildSelectEndpointsForEvent(s.dialect, eventType)

	endpoints, err := s.scanEndpoints(ctx, q, query, args)
	if err != nil {
		return nil, platformerrors.Wrapf(err, "reading webhook endpoints for event %q", eventType)
	}

	return endpoints, nil
}

// Enqueue writes the delivery and its dispatches through the caller's executor,
// so they commit with whatever else that transaction did.
func (s *sqlStore) Enqueue(ctx context.Context, q database.SQLQueryExecutor, delivery *Delivery, endpointIDs []string, now time.Time) error {
	if q == nil {
		return ErrNilExecutor
	}

	if delivery == nil {
		return ErrNilDelivery
	}

	if len(endpointIDs) == 0 {
		return nil
	}

	query, args := s.tables.buildInsertDelivery(s.dialect, delivery, now)
	if _, err := q.ExecContext(ctx, query, args...); err != nil {
		return platformerrors.Wrap(err, "inserting webhook delivery")
	}

	rows := make([]dispatchRow, 0, len(endpointIDs))
	for _, endpointID := range endpointIDs {
		rows = append(rows, dispatchRow{
			id:          identifiers.New(),
			deliveryID:  delivery.ID,
			endpointID:  endpointID,
			orderingKey: delivery.OrderingKey,
			createdAt:   now,
		})
	}

	query, args = s.tables.buildInsertDispatches(s.dialect, rows)
	if _, err := q.ExecContext(ctx, query, args...); err != nil {
		return platformerrors.Wrap(err, "inserting webhook dispatches")
	}

	return nil
}

// Claim selects a batch, leases it, and reads it back — all in one transaction,
// so two workers cannot lease the same rows.
func (s *sqlStore) Claim(ctx context.Context, now time.Time, limit int, leaseUntil time.Time) ([]ClaimedDispatch, error) {
	var claimed []ClaimedDispatch

	err := s.client.WithTransaction(ctx, func(q database.SQLQueryExecutor) error {
		selectQuery, selectArgs := s.tables.buildSelectClaimable(
			s.dialect, now.UTC(), limit, s.dialect.SupportsSkipLocked(),
		)

		ids, err := scanIDs(ctx, q, selectQuery, selectArgs)
		if err != nil {
			return platformerrors.Wrap(err, "selecting claimable webhook dispatches")
		}

		if len(ids) == 0 {
			return nil
		}

		claimQuery, claimArgs := s.tables.buildClaim(s.dialect, ids, leaseUntil.UTC())
		if _, err = q.ExecContext(ctx, claimQuery, claimArgs...); err != nil {
			return platformerrors.Wrap(err, "claiming webhook dispatches")
		}

		fetchQuery, fetchArgs := s.tables.buildFetchClaimed(s.dialect, ids)

		// A dispatch whose endpoint was disabled or archived between fan-out and
		// claim is filtered out by the fetch join, so it is claimed here and
		// simply not returned. Its lease expires and it is reclaimed, which is a
		// slow no-op rather than a delivery — the alternative, delivering to an
		// endpoint an operator has just disabled, is worse.
		if claimed, err = s.scanClaimed(ctx, q, fetchQuery, fetchArgs); err != nil {
			return platformerrors.Wrap(err, "reading claimed webhook dispatches")
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return claimed, nil
}

// MarkDelivered retires an accepted dispatch.
func (s *sqlStore) MarkDelivered(ctx context.Context, dispatchID string, at time.Time) error {
	query, args := s.tables.buildMarkDelivered(s.dialect, dispatchID, at.UTC())

	if _, err := s.client.Writer().ExecContext(ctx, query, args...); err != nil {
		return platformerrors.Wrapf(err, "marking webhook dispatch %q delivered", dispatchID)
	}

	return nil
}

// RecordFailure schedules the retry, or marks the dispatch dead.
func (s *sqlStore) RecordFailure(ctx context.Context, dispatchID string, attempts int, nextAttempt time.Time, lastErr string, dead bool) error {
	if attempts < 0 {
		attempts = 0
	}

	query, args := s.tables.buildRecordFailure(s.dialect, dispatchID, attempts, nextAttempt.UTC(), lastErr, dead)

	if _, err := s.client.Writer().ExecContext(ctx, query, args...); err != nil {
		return platformerrors.Wrapf(err, "recording webhook dispatch %q failure", dispatchID)
	}

	return nil
}

// RecordAttempt appends to the delivery log.
func (s *sqlStore) RecordAttempt(ctx context.Context, attempt *Attempt) error {
	if attempt == nil {
		return platformerrors.Wrap(platformerrors.ErrNilInputParameter, "nil webhook attempt")
	}

	if attempt.ID == "" {
		attempt.ID = identifiers.New()
	}

	query, args := s.tables.buildInsertAttempt(s.dialect, attempt)

	if _, err := s.client.Writer().ExecContext(ctx, query, args...); err != nil {
		return platformerrors.Wrap(err, "recording webhook delivery attempt")
	}

	return nil
}

// ListAttempts pages one delivery's log.
func (s *sqlStore) ListAttempts(ctx context.Context, deliveryID string, filter *filtering.QueryFilter) (*filtering.QueryFilteredResult[Attempt], error) {
	if filter == nil {
		filter = filtering.DefaultQueryFilter()
	}

	limit := filtering.DefaultQueryFilterLimit
	if filter.MaxResponseSize != nil && *filter.MaxResponseSize > 0 {
		limit = int(*filter.MaxResponseSize)
	}

	var cursor string
	if filter.Cursor != nil {
		cursor = *filter.Cursor
	}

	query, args := s.tables.buildListAttempts(s.dialect, deliveryID, cursor, limit)

	attempts, err := s.scanAttempts(ctx, query, args)
	if err != nil {
		return nil, platformerrors.Wrapf(err, "listing webhook attempts for delivery %q", deliveryID)
	}

	countQuery, countArgs := s.tables.buildCountAttempts(s.dialect, deliveryID)

	var total uint64
	if err = s.client.Reader().QueryRowContext(ctx, countQuery, countArgs...).Scan(&total); err != nil {
		return nil, platformerrors.Wrap(err, "counting webhook attempts")
	}

	return filtering.NewQueryFilteredResult(
		attempts, uint64(len(attempts)), total,
		func(a *Attempt) string { return a.ID },
		filter,
	), nil
}

// Requeue re-drives one delivery to one endpoint.
func (s *sqlStore) Requeue(ctx context.Context, deliveryID, endpointID string, at time.Time) error {
	query, args := s.tables.buildRequeue(s.dialect, deliveryID, endpointID, at.UTC())

	res, err := s.client.Writer().ExecContext(ctx, query, args...)
	if err != nil {
		return platformerrors.Wrapf(err, "requeuing webhook delivery %q to endpoint %q", deliveryID, endpointID)
	}

	affected, err := res.RowsAffected()
	if err != nil {
		// The UPDATE ran; only the count is unavailable, which some drivers
		// simply do not report. Reporting success is right — the replay
		// happened, and the caller learns the outcome from the attempts log
		// either way. The alternative would fail a replay that worked.
		return nil //nolint:nilerr // the write succeeded; only the row count is unavailable
	}

	if affected == 0 {
		return platformerrors.Wrapf(ErrDeliveryNotFound, "delivery %q to endpoint %q", deliveryID, endpointID)
	}

	return nil
}

// Backlog reads how many dispatches are waiting and how old the oldest is.
func (s *sqlStore) Backlog(ctx context.Context) (depth int64, oldest time.Time, err error) {
	var raw any
	if err = s.client.Reader().QueryRowContext(ctx, s.tables.buildBacklog()).Scan(&depth, &raw); err != nil {
		return 0, time.Time{}, platformerrors.Wrap(err, "reading webhook backlog")
	}

	created, ok := coerceTime(raw)
	if !ok {
		return depth, time.Time{}, nil
	}

	return depth, created.UTC(), nil
}

// Reap deletes delivered dispatches past the retention window, then the log
// rows and deliveries left without one.
//
// The three DELETEs run in one transaction so a crash between them cannot leave
// a delivery whose dispatches are gone but whose payload lingers forever —
// nothing would ever revisit it.
func (s *sqlStore) Reap(ctx context.Context, before time.Time, limit int) (int64, error) {
	var reaped int64

	err := s.client.WithTransaction(ctx, func(q database.SQLQueryExecutor) error {
		query, args := s.tables.buildReapDispatches(s.dialect, before.UTC(), limit)

		res, err := q.ExecContext(ctx, query, args...)
		if err != nil {
			return platformerrors.Wrap(err, "reaping webhook dispatches")
		}

		if reaped, err = res.RowsAffected(); err != nil {
			reaped = 0
		}

		if reaped == 0 {
			return nil
		}

		query, args = s.tables.buildReapAttempts(s.dialect, limit)
		if _, err = q.ExecContext(ctx, query, args...); err != nil {
			return platformerrors.Wrap(err, "reaping webhook attempts")
		}

		query, args = s.tables.buildReapDeliveries(s.dialect, limit)
		if _, err = q.ExecContext(ctx, query, args...); err != nil {
			return platformerrors.Wrap(err, "reaping webhook deliveries")
		}

		return nil
	})
	if err != nil {
		return 0, err
	}

	return reaped, nil
}

// subscriptionsFor reads one endpoint's event types.
func (s *sqlStore) subscriptionsFor(ctx context.Context, q database.SQLQueryExecutor, endpointID string) (events []string, err error) {
	query := "SELECT event_type FROM " + s.tables.subscriptions +
		" WHERE endpoint_id = " + s.dialect.Placeholder(1) + " ORDER BY event_type"

	rows, err := q.QueryContext(ctx, query, endpointID)
	if err != nil {
		return nil, platformerrors.Wrapf(err, "reading subscriptions for webhook endpoint %q", endpointID)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = platformerrors.Wrap(closeErr, "closing webhook subscription rows")
		}
	}()

	for rows.Next() {
		var event string
		if err = rows.Scan(&event); err != nil {
			return nil, err
		}

		events = append(events, event)
	}

	return events, rows.Err()
}

// scanEndpoints projects endpoint rows, without their subscriptions.
func (s *sqlStore) scanEndpoints(ctx context.Context, q database.SQLQueryExecutor, query string, args []any) (endpoints []*Endpoint, err error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = platformerrors.Wrap(closeErr, "closing webhook endpoint rows")
		}
	}()

	for rows.Next() {
		endpoint, scanErr := scanEndpoint(rows)
		if scanErr != nil {
			return nil, scanErr
		}

		endpoints = append(endpoints, endpoint)
	}

	return endpoints, rows.Err()
}

// scanEndpoint reads one endpoint row. The column list comes from
// endpointColumns so the query and this scan cannot drift.
func scanEndpoint(scanner database.Scanner) (*Endpoint, error) {
	var (
		endpoint Endpoint
		previous []byte
		headers  []byte
	)

	if err := scanner.Scan(
		&endpoint.ID, &endpoint.URL, &endpoint.ContentType,
		&endpoint.Secret.Current, &previous, &headers, &endpoint.Disabled,
	); err != nil {
		return nil, err
	}

	endpoint.Secret.Previous = previous

	// An endpoint registered before any headers were stored, or one whose
	// headers column holds a JSON null, yields a nil map rather than an error.
	if len(headers) > 0 {
		if err := json.Unmarshal(headers, &endpoint.Headers); err != nil {
			return nil, platformerrors.Wrapf(err, "unmarshaling headers for webhook endpoint %q", endpoint.ID)
		}
	}

	return &endpoint, nil
}

// scanClaimed projects claimed dispatches joined to their delivery and
// endpoint. The column list comes from dispatchColumns.
func (s *sqlStore) scanClaimed(ctx context.Context, q database.SQLQueryExecutor, query string, args []any) (claimed []ClaimedDispatch, err error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = platformerrors.Wrap(closeErr, "closing claimed webhook dispatch rows")
		}
	}()

	for rows.Next() {
		var (
			row         ClaimedDispatch
			endpoint    Endpoint
			orderingKey sql.NullString
			previous    []byte
			headers     []byte
		)

		if err = rows.Scan(
			&row.ID, &row.DeliveryID, &row.EndpointID, &orderingKey, &row.Attempts,
			&row.EventType, &row.Payload,
			&endpoint.ID, &endpoint.URL, &endpoint.ContentType,
			&endpoint.Secret.Current, &previous, &headers, &endpoint.Disabled,
		); err != nil {
			return nil, err
		}

		endpoint.Secret.Previous = previous

		if len(headers) > 0 {
			if err = json.Unmarshal(headers, &endpoint.Headers); err != nil {
				return nil, platformerrors.Wrapf(err, "unmarshaling headers for webhook endpoint %q", endpoint.ID)
			}
		}

		row.OrderingKey = orderingKey.String
		row.Endpoint = &endpoint

		claimed = append(claimed, row)
	}

	return claimed, rows.Err()
}

// scanAttempts projects delivery log rows. The column list comes from
// attemptColumns.
func (s *sqlStore) scanAttempts(ctx context.Context, query string, args []any) (attempts []*Attempt, err error) {
	rows, err := s.client.Reader().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = platformerrors.Wrap(closeErr, "closing webhook attempt rows")
		}
	}()

	for rows.Next() {
		var (
			attempt     Attempt
			failure     sql.NullString
			durationMS  int64
			attemptedAt any
		)

		if err = rows.Scan(
			&attempt.ID, &attempt.DeliveryID, &attempt.EndpointID, &attempt.AttemptCount,
			&attempt.StatusCode, &failure, &durationMS, &attemptedAt,
		); err != nil {
			return nil, err
		}

		attempt.Error = failure.String
		attempt.Duration = time.Duration(durationMS) * time.Millisecond

		if at, ok := coerceTime(attemptedAt); ok {
			attempt.AttemptedAt = at.UTC()
		}

		attempts = append(attempts, &attempt)
	}

	return attempts, rows.Err()
}

// scanIDs runs a single-column query and collects the results. A close failure
// is surfaced only when nothing worse already went wrong, so the real cause is
// never masked by the cleanup.
func scanIDs(ctx context.Context, q database.SQLQueryExecutor, query string, args []any) (ids []string, err error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = platformerrors.Wrap(closeErr, "closing webhook dispatch id rows")
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

// coerceTime normalizes whatever a driver hands back for a timestamp read as
// `any`.
//
// Timestamps are scanned as `any` rather than sql.NullTime because the drivers
// disagree. pgx and go-sql-driver return a time.Time, but modernc's SQLite
// driver stores a bound time.Time as Go's own String() rendering, and an
// aggregate over such a column loses the declared DATETIME affinity — so it
// comes back as a plain string that sql.NullTime refuses outright.
//
// A NULL reports false, and callers treat that as "no value" rather than as the
// zero time.
func coerceTime(v any) (time.Time, bool) {
	var s string

	switch typed := v.(type) {
	case nil:
		return time.Time{}, false
	case time.Time:
		return typed, true
	case string:
		s = typed
	case []byte:
		s = string(typed)
	default:
		return time.Time{}, false
	}

	// Go's String() layout comes first: it is what the SQLite path actually
	// produces, and the others are here so a driver change does not silently
	// zero the value.
	for _, layout := range []string{
		"2006-01-02 15:04:05.999999999 -0700 MST",
		time.RFC3339Nano,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
	} {
		if parsed, parseErr := time.Parse(layout, s); parseErr == nil {
			return parsed, true
		}
	}

	return time.Time{}, false
}
