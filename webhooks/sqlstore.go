package webhooks

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/primandproper/platform-go/v10/clock"
	"github.com/primandproper/platform-go/v10/database"
	"github.com/primandproper/platform-go/v10/database/dialect"
	platformerrors "github.com/primandproper/platform-go/v10/errors"
	"github.com/primandproper/platform-go/v10/filtering"
	"github.com/primandproper/platform-go/v10/identifiers"
	"github.com/primandproper/platform-go/v10/observability"
	"github.com/primandproper/platform-go/v10/observability/logging"
	"github.com/primandproper/platform-go/v10/observability/metrics"
	"github.com/primandproper/platform-go/v10/observability/tracing"
	"github.com/primandproper/platform-go/v10/webhooks/migrations"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// DefaultTablePrefix is the namespace the webhooks tables carry when none is
// configured, which is none — rendering webhooks_endpoints and its four siblings.
//
// The webhooks_ segment is the schema's, not the caller's: a table always says
// which package created it. Setting a namespace of "ddb" renders
// ddb_webhooks_endpoints, for a database shared between applications. A namespace must
// not end in '_'; database/ddl supplies the separator.
const DefaultTablePrefix = ""

// storeName scopes the store's spans and logger. It is deliberately not
// serviceName: a trace showing a delivery going out and the rows that moved
// wants those distinguishable, and one scope for both would make a claim read
// like an HTTP call in every span listing.
const storeName = serviceName + "_store"

var _ Store = (*SQLStore)(nil)

// SQLStore is the SQL-backed Store, against the schema webhooks/migrations
// renders.
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
// to a noop logger, traces to a noop provider, and counts into a noop meter.
func NewSQLStore(client database.Client, opts ...SQLStoreOption) (*SQLStore, error) {
	if client == nil {
		return nil, ErrNilDatabaseClient
	}

	d := client.Dialect()
	if !d.Valid() {
		return nil, platformerrors.Wrapf(dialect.ErrUnsupported, "webhooks dialect %q", d)
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

	// One counter, and it is the one nothing above this layer can see. The
	// Worker owns the delivery totals; what it cannot know is that a driver
	// declined to report how many rows a write touched, which this store has to
	// absorb in two places. A requeue then reports success without having
	// confirmed one, and a reap reports zero without having reaped zero — both
	// correct answers to give, and both indistinguishable from the real thing
	// unless somebody is counting.
	mp := metrics.EnsureMetricsProvider(s.metricsProvider)

	var err error
	if s.unreportedRowsCounter, err = mp.NewInt64Counter(storeName + "_unreported_row_counts"); err != nil {
		return nil, platformerrors.Wrap(err, "creating webhooks store unreported row count counter")
	}

	return s, nil
}

// storeOpAttr labels an unreported row count with the operation it happened in,
// since the two places it can happen mean different things.
func storeOpAttr(operation string) metric.MeasurementOption {
	return metric.WithAttributes(attribute.String(storeOpKey, operation))
}

// ErrNilDatabaseClient indicates a nil database.Client. It wraps
// errors.ErrNilInputParameter, so a caller may check either.
var ErrNilDatabaseClient = platformerrors.Wrap(platformerrors.ErrNilInputParameter, "nil database client")

// SaveEndpoint upserts the endpoint and replaces its subscription set, both in
// one transaction — a half-registered endpoint would either receive events it
// no longer subscribes to or silently receive none.
func (s *SQLStore) SaveEndpoint(ctx context.Context, endpoint *Endpoint) error {
	ctx, op := s.o11y.Begin(ctx)
	defer op.End()

	if endpoint == nil {
		return op.Error(ErrNilEndpoint, "saving webhook endpoint")
	}

	op.Set(endpointIDKey, endpoint.ID).Set(endpointURLKey, endpoint.URL)

	headers, err := json.Marshal(endpoint.Headers)
	if err != nil {
		return op.Error(err, "marshaling webhook endpoint headers")
	}

	now := s.clock.Now().UTC()

	if err = s.client.WithTransaction(ctx, func(q database.SQLQueryExecutor) error {
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
	}); err != nil {
		return op.Error(err, "saving webhook endpoint")
	}

	return nil
}

// GetEndpoint reads one endpoint and its subscriptions.
func (s *SQLStore) GetEndpoint(ctx context.Context, endpointID string) (*Endpoint, error) {
	ctx, op := s.o11y.Begin(ctx, observability.WithValue(endpointIDKey, endpointID))
	defer op.End()

	query, args := s.tables.buildSelectEndpoint(s.dialect, endpointID)

	endpoint, err := scanEndpoint(s.client.Reader().QueryRowContext(ctx, query, args...))
	if err != nil {
		return nil, op.Error(err, "reading webhook endpoint %q", endpointID)
	}

	if endpoint.Events, err = s.subscriptionsFor(ctx, s.client.Reader(), endpointID); err != nil {
		return nil, op.Error(err, "reading webhook endpoint %q subscriptions", endpointID)
	}

	return endpoint, nil
}

// ListEndpoints pages the registry.
func (s *SQLStore) ListEndpoints(ctx context.Context, filter *filtering.QueryFilter) (*filtering.QueryFilteredResult[Endpoint], error) {
	ctx, op := s.o11y.Begin(ctx)
	defer op.End()

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
		return nil, op.Error(err, "listing webhook endpoints")
	}

	// Subscriptions are read per endpoint rather than through a join, so that
	// one endpoint with thirty event types does not multiply every other row in
	// the page by its subscription count.
	for _, endpoint := range endpoints {
		if endpoint.Events, err = s.subscriptionsFor(ctx, s.client.Reader(), endpoint.ID); err != nil {
			return nil, op.Error(err, "listing webhook endpoints")
		}
	}

	var total uint64
	if err = s.client.Reader().QueryRowContext(ctx, s.tables.buildCountEndpoints()).Scan(&total); err != nil {
		return nil, op.Error(err, "counting webhook endpoints")
	}

	op.SpanOnly(endpointCountKey, len(endpoints))

	return filtering.NewQueryFilteredResult(
		endpoints, uint64(len(endpoints)), total,
		func(e *Endpoint) string { return e.ID },
		filter,
	), nil
}

// ArchiveEndpoint retires an endpoint.
func (s *SQLStore) ArchiveEndpoint(ctx context.Context, endpointID string) error {
	ctx, op := s.o11y.Begin(ctx, observability.WithValue(endpointIDKey, endpointID))
	defer op.End()

	query, args := s.tables.buildArchiveEndpoint(s.dialect, endpointID, s.clock.Now().UTC())

	if _, err := s.client.Writer().ExecContext(ctx, query, args...); err != nil {
		return op.Error(err, "archiving webhook endpoint %q", endpointID)
	}

	return nil
}

// EndpointsForEvent resolves the fan-out set, using the caller's executor so it
// sees the same snapshot as the transaction that is dispatching.
func (s *SQLStore) EndpointsForEvent(ctx context.Context, q database.SQLQueryExecutor, eventType string) ([]*Endpoint, error) {
	ctx, op := s.o11y.Begin(ctx, observability.WithValue(eventTypeKey, eventType))
	defer op.End()

	if q == nil {
		return nil, op.Error(ErrNilExecutor, "reading webhook endpoints for event %q", eventType)
	}

	query, args := s.tables.buildSelectEndpointsForEvent(s.dialect, eventType)

	endpoints, err := s.scanEndpoints(ctx, q, query, args)
	if err != nil {
		return nil, op.Error(err, "reading webhook endpoints for event %q", eventType)
	}

	op.SpanOnly(endpointCountKey, len(endpoints))

	return endpoints, nil
}

// Enqueue writes the delivery and its dispatches through the caller's executor,
// so they commit with whatever else that transaction did.
func (s *SQLStore) Enqueue(ctx context.Context, q database.SQLQueryExecutor, delivery *Delivery, endpointIDs []string, now time.Time) error {
	ctx, op := s.o11y.Begin(ctx, observability.WithValue(fanoutKey, len(endpointIDs)))
	defer op.End()

	if q == nil {
		return op.Error(ErrNilExecutor, "enqueuing webhook delivery")
	}

	if delivery == nil {
		return op.Error(ErrNilDelivery, "enqueuing webhook delivery")
	}

	op.Set(deliveryIDKey, delivery.ID).Set(eventTypeKey, delivery.EventType)

	if len(endpointIDs) == 0 {
		return nil
	}

	query, args := s.tables.buildInsertDelivery(s.dialect, delivery, now)
	if _, err := q.ExecContext(ctx, query, args...); err != nil {
		return op.Error(err, "inserting webhook delivery")
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
		return op.Error(err, "inserting webhook dispatches")
	}

	return nil
}

// Claim selects a batch, leases it, and reads it back — all in one transaction,
// so two workers cannot lease the same rows.
func (s *SQLStore) Claim(ctx context.Context, now time.Time, limit int, leaseUntil time.Time) ([]ClaimedDispatch, error) {
	ctx, op := s.o11y.Begin(ctx, observability.WithValue(limitKey, limit))
	defer op.End()

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
		return nil, op.Error(err, "claiming webhook dispatches")
	}

	op.SpanOnly(claimedKey, len(claimed))

	return claimed, nil
}

// MarkDelivered retires an accepted dispatch.
func (s *SQLStore) MarkDelivered(ctx context.Context, dispatchID string, at time.Time) error {
	ctx, op := s.o11y.Begin(ctx, observability.WithValue(dispatchIDKey, dispatchID))
	defer op.End()

	query, args := s.tables.buildMarkDelivered(s.dialect, dispatchID, at.UTC())

	if _, err := s.client.Writer().ExecContext(ctx, query, args...); err != nil {
		return op.Error(err, "marking webhook dispatch %q delivered", dispatchID)
	}

	return nil
}

// RecordFailure schedules the retry, or marks the dispatch dead.
func (s *SQLStore) RecordFailure(ctx context.Context, dispatchID string, attempts int, nextAttempt time.Time, lastErr string, dead bool) error {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(dispatchIDKey, dispatchID),
		observability.WithValue(deadKey, dead),
	)
	defer op.End()

	if attempts < 0 {
		attempts = 0
	}

	op.SpanOnly(attemptsKey, attempts)

	query, args := s.tables.buildRecordFailure(s.dialect, dispatchID, attempts, nextAttempt.UTC(), lastErr, dead)

	if _, err := s.client.Writer().ExecContext(ctx, query, args...); err != nil {
		return op.Error(err, "recording webhook dispatch %q failure", dispatchID)
	}

	return nil
}

// RecordAttempt appends to the delivery log.
func (s *SQLStore) RecordAttempt(ctx context.Context, attempt *Attempt) error {
	ctx, op := s.o11y.Begin(ctx)
	defer op.End()

	if attempt == nil {
		return op.Error(platformerrors.ErrNilInputParameter, "nil webhook attempt")
	}

	if attempt.ID == "" {
		attempt.ID = identifiers.New()
	}

	op.Set(deliveryIDKey, attempt.DeliveryID).
		Set(endpointIDKey, attempt.EndpointID).
		SpanOnly(statusCodeKey, attempt.StatusCode)

	query, args := s.tables.buildInsertAttempt(s.dialect, attempt)

	if _, err := s.client.Writer().ExecContext(ctx, query, args...); err != nil {
		return op.Error(err, "recording webhook delivery attempt")
	}

	return nil
}

// ListAttempts pages one delivery's log.
func (s *SQLStore) ListAttempts(ctx context.Context, deliveryID string, filter *filtering.QueryFilter) (*filtering.QueryFilteredResult[Attempt], error) {
	ctx, op := s.o11y.Begin(ctx, observability.WithValue(deliveryIDKey, deliveryID))
	defer op.End()

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
		return nil, op.Error(err, "listing webhook attempts for delivery %q", deliveryID)
	}

	countQuery, countArgs := s.tables.buildCountAttempts(s.dialect, deliveryID)

	var total uint64
	if err = s.client.Reader().QueryRowContext(ctx, countQuery, countArgs...).Scan(&total); err != nil {
		return nil, op.Error(err, "counting webhook attempts")
	}

	op.SpanOnly(attemptCountKey, len(attempts))

	return filtering.NewQueryFilteredResult(
		attempts, uint64(len(attempts)), total,
		func(a *Attempt) string { return a.ID },
		filter,
	), nil
}

// Requeue re-drives one delivery to one endpoint.
func (s *SQLStore) Requeue(ctx context.Context, deliveryID, endpointID string, at time.Time) error {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(deliveryIDKey, deliveryID),
		observability.WithValue(endpointIDKey, endpointID),
	)
	defer op.End()

	query, args := s.tables.buildRequeue(s.dialect, deliveryID, endpointID, at.UTC())

	res, err := s.client.Writer().ExecContext(ctx, query, args...)
	if err != nil {
		return op.Error(err, "requeuing webhook delivery %q to endpoint %q", deliveryID, endpointID)
	}

	affected, err := res.RowsAffected()
	if err != nil {
		// The UPDATE ran; only the count is unavailable, which some drivers
		// simply do not report. Reporting success is right — the replay
		// happened, and the caller learns the outcome from the attempts log
		// either way. The alternative would fail a replay that worked.
		//
		// Acknowledged rather than returned, and counted, because from here it
		// is indistinguishable from a requeue that matched a row.
		op.Acknowledge(err, "reading rows affected by webhook requeue")
		s.unreportedRowsCounter.Add(ctx, 1, storeOpAttr("requeue"))

		return nil
	}

	if affected == 0 {
		return op.Error(ErrDeliveryNotFound, "delivery %q to endpoint %q", deliveryID, endpointID)
	}

	return nil
}

// Backlog reads how many dispatches are waiting and how old the oldest is.
func (s *SQLStore) Backlog(ctx context.Context) (depth int64, oldest time.Time, err error) {
	ctx, op := s.o11y.Begin(ctx)
	defer op.End()

	var raw any
	if err = s.client.Reader().QueryRowContext(ctx, s.tables.buildBacklog()).Scan(&depth, &raw); err != nil {
		return 0, time.Time{}, op.Error(err, "reading webhook backlog")
	}

	op.SpanOnly(backlogDepthKey, depth)

	created, ok := database.CoerceTime(raw)
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
func (s *SQLStore) Reap(ctx context.Context, before time.Time, limit int) (int64, error) {
	ctx, op := s.o11y.Begin(ctx, observability.WithValue(limitKey, limit))
	defer op.End()

	var reaped int64

	err := s.client.WithTransaction(ctx, func(q database.SQLQueryExecutor) error {
		query, args := s.tables.buildReapDispatches(s.dialect, before.UTC(), limit)

		res, err := q.ExecContext(ctx, query, args...)
		if err != nil {
			return platformerrors.Wrap(err, "reaping webhook dispatches")
		}

		// A driver that cannot report row counts is not the same as a reap that
		// deleted nothing, and conflating the two skipped the two dependent
		// DELETEs below on every cycle — so the attempts and deliveries tables grew
		// without bound on exactly the drivers that cannot tell you they are.
		affected, rowsErr := res.RowsAffected()
		switch {
		case rowsErr != nil:
			// Count unavailable: press on, and report 0 rather than a number this
			// driver never gave us. Counted, because a reap that reports 0
			// forever and a backlog that never shrinks look the same from above.
			op.Acknowledge(rowsErr, "reading rows affected by webhook dispatch reap")
			s.unreportedRowsCounter.Add(ctx, 1, storeOpAttr("reap"))

			reaped = 0
		case affected == 0:
			// Nothing aged out, so there is nothing orphaned to collect either.
			return nil
		default:
			reaped = affected
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
		return 0, op.Error(err, "reaping webhook dispatches")
	}

	op.SpanOnly(reapedKey, reaped)

	return reaped, nil
}

// subscriptionsFor reads one endpoint's event types.
func (s *SQLStore) subscriptionsFor(ctx context.Context, q database.SQLQueryExecutor, endpointID string) ([]string, error) {
	query := "SELECT event_type FROM " + s.tables.subscriptions +
		" WHERE endpoint_id = " + s.dialect.Placeholder(1) + " ORDER BY event_type"

	events, err := database.ScanStrings(ctx, q, "webhook subscription", query, []any{endpointID})
	if err != nil {
		return nil, platformerrors.Wrapf(err, "reading subscriptions for webhook endpoint %q", endpointID)
	}

	return events, nil
}

// scanEndpoints projects endpoint rows, without their subscriptions.
func (s *SQLStore) scanEndpoints(ctx context.Context, q database.SQLQueryExecutor, query string, args []any) ([]*Endpoint, error) {
	return database.ScanAll(ctx, q, "webhook endpoint", query, args, scanEndpoint)
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
func (s *SQLStore) scanClaimed(ctx context.Context, q database.SQLQueryExecutor, query string, args []any) ([]ClaimedDispatch, error) {
	return database.ScanAll(ctx, q, "claimed webhook dispatch", query, args, func(scanner database.Scanner) (ClaimedDispatch, error) {
		var (
			row         ClaimedDispatch
			endpoint    Endpoint
			orderingKey sql.NullString
			previous    []byte
			headers     []byte
		)

		if err := scanner.Scan(
			&row.ID, &row.DeliveryID, &row.EndpointID, &orderingKey, &row.Attempts,
			&row.EventType, &row.Payload,
			&endpoint.ID, &endpoint.URL, &endpoint.ContentType,
			&endpoint.Secret.Current, &previous, &headers, &endpoint.Disabled,
		); err != nil {
			return ClaimedDispatch{}, err
		}

		endpoint.Secret.Previous = previous

		if len(headers) > 0 {
			if err := json.Unmarshal(headers, &endpoint.Headers); err != nil {
				return ClaimedDispatch{}, platformerrors.Wrapf(err, "unmarshaling headers for webhook endpoint %q", endpoint.ID)
			}
		}

		row.OrderingKey = orderingKey.String
		row.Endpoint = &endpoint

		return row, nil
	})
}

// scanAttempts projects delivery log rows. The column list comes from
// attemptColumns.
func (s *SQLStore) scanAttempts(ctx context.Context, query string, args []any) ([]*Attempt, error) {
	return database.ScanAll(ctx, s.client.Reader(), "webhook attempt", query, args, func(scanner database.Scanner) (*Attempt, error) {
		var (
			attempt     Attempt
			failure     sql.NullString
			durationMS  int64
			attemptedAt any
		)

		if err := scanner.Scan(
			&attempt.ID, &attempt.DeliveryID, &attempt.EndpointID, &attempt.AttemptCount,
			&attempt.StatusCode, &failure, &durationMS, &attemptedAt,
		); err != nil {
			return nil, err
		}

		attempt.Error = failure.String
		attempt.Duration = time.Duration(durationMS) * time.Millisecond

		if at, ok := database.CoerceTime(attemptedAt); ok {
			attempt.AttemptedAt = at.UTC()
		}

		return &attempt, nil
	})
}

// scanIDs runs a single-column query and collects the results. A close failure
// is surfaced only when nothing worse already went wrong, so the real cause is
// never masked by the cleanup.
func scanIDs(ctx context.Context, q database.SQLQueryExecutor, query string, args []any) ([]string, error) {
	return database.ScanStrings(ctx, q, "webhook dispatch id", query, args)
}
