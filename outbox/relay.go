package outbox

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/primandproper/platform-go/v9/clock"
	"github.com/primandproper/platform-go/v9/database"
	"github.com/primandproper/platform-go/v9/database/dialect"
	platformerrors "github.com/primandproper/platform-go/v9/errors"
	"github.com/primandproper/platform-go/v9/messagequeue"
	"github.com/primandproper/platform-go/v9/observability"
	"github.com/primandproper/platform-go/v9/observability/keys"
	"github.com/primandproper/platform-go/v9/observability/logging"
	"github.com/primandproper/platform-go/v9/observability/metrics"
	"github.com/primandproper/platform-go/v9/observability/tracing"
	"github.com/primandproper/platform-go/v9/retry"

	validation "github.com/go-ozzo/ozzo-validation/v4"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const (
	// DefaultBatchSize is how many messages one cycle claims.
	DefaultBatchSize = 100
	// DefaultPollInterval is how often the relay looks for work.
	DefaultPollInterval = time.Second
	// DefaultLeaseDuration is how long a claim is held before another relay may
	// reclaim the message. It must comfortably exceed the time to publish a
	// batch, or two relays will publish the same message concurrently.
	DefaultLeaseDuration = 30 * time.Second
	// DefaultRetention is how long published rows are kept before reaping.
	DefaultRetention = 24 * time.Hour
	// DefaultReapInterval is how often the reaper runs.
	DefaultReapInterval = 5 * time.Minute
	// DefaultReapBatchSize caps one reap, so a large backlog is removed over
	// several passes instead of one long-running DELETE.
	DefaultReapBatchSize = 1000
)

// Observability keys for this package's spans and log fields. Declared once so
// that a field set on a span and the same field logged alongside it cannot
// drift apart, and so the outbox. prefix is applied uniformly — an un-namespaced
// attribute name collides with every other component writing to the same trace.
//
// Keys that are not outbox-specific come from observability/keys instead;
// keys.TopicKey is the one in use here.
const (
	messageIDKey       = "outbox.message_id"
	messageCountKey    = "outbox.message_count"
	partitionKeyKey    = "outbox.partition_key"
	attemptsKey        = "outbox.attempts"
	claimedKey         = "outbox.claimed"
	claimModeKey       = "outbox.claim_mode"
	batchSizeKey       = "outbox.batch_size"
	backlogDepthKey    = "outbox.backlog_depth"
	backlogAgeKey      = "outbox.backlog_age_seconds"
	retentionCutoffKey = "outbox.retention_cutoff"
	reapedKey          = "outbox.reaped"
)

// ClaimMode selects how the relay takes ownership of messages.
type ClaimMode string

const (
	// ClaimSkipLocked claims with FOR UPDATE SKIP LOCKED, so several relays can
	// run at once without contending. Requires Postgres or MySQL.
	ClaimSkipLocked ClaimMode = "skip_locked"
	// ClaimLease claims with a lease alone. Correct everywhere — and the only
	// option on SQLite — and the right choice when a single relay is running.
	ClaimLease ClaimMode = "lease"
)

// Valid reports whether m is a known claim mode.
func (m ClaimMode) Valid() bool {
	return m == ClaimSkipLocked || m == ClaimLease
}

// RelayConfig configures a Relay.
type RelayConfig struct {
	// Dialect selects the SQL emitted; it must match the database.Client.
	Dialect dialect.Dialect `env:"DIALECT" json:"dialect" yaml:"dialect"`
	// TableName is the outbox table. Defaults to DefaultTableName.
	TableName string `env:"TABLE_NAME" json:"tableName" yaml:"tableName"`
	// ClaimMode selects lease-only or SKIP LOCKED claiming.
	ClaimMode ClaimMode `env:"CLAIM_MODE" json:"claimMode" yaml:"claimMode"`
	// Backoff drives the retry schedule for messages that fail to publish.
	// MaxAttempts is the quarantine threshold.
	Backoff retry.Config `envPrefix:"BACKOFF_" json:"backoff" yaml:"backoff"`
	// BatchSize is how many messages one cycle claims.
	BatchSize int `env:"BATCH_SIZE" json:"batchSize" yaml:"batchSize"`
	// PollInterval is how often the relay looks for work.
	PollInterval time.Duration `env:"POLL_INTERVAL" json:"pollInterval" yaml:"pollInterval"`
	// LeaseDuration is how long a claim is held before it can be reclaimed.
	LeaseDuration time.Duration `env:"LEASE_DURATION" json:"leaseDuration" yaml:"leaseDuration"`
	// Retention is how long published rows are kept before reaping.
	Retention time.Duration `env:"RETENTION" json:"retention" yaml:"retention"`
	// ReapInterval is how often the reaper runs.
	ReapInterval time.Duration `env:"REAP_INTERVAL" json:"reapInterval" yaml:"reapInterval"`
	// ReapBatchSize caps how many rows one reap deletes.
	ReapBatchSize int `env:"REAP_BATCH_SIZE" json:"reapBatchSize" yaml:"reapBatchSize"`
}

var _ validation.ValidatableWithContext = (*RelayConfig)(nil)

// EnsureDefaults fills unset knobs with the package defaults. SQLite is forced
// to ClaimLease because it has no SKIP LOCKED.
func (cfg *RelayConfig) EnsureDefaults() {
	if cfg.TableName == "" {
		cfg.TableName = DefaultTableName
	}
	if cfg.ClaimMode == "" {
		cfg.ClaimMode = ClaimSkipLocked
	}
	if !cfg.Dialect.SupportsSkipLocked() {
		cfg.ClaimMode = ClaimLease
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = DefaultBatchSize
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = DefaultPollInterval
	}
	if cfg.LeaseDuration <= 0 {
		cfg.LeaseDuration = DefaultLeaseDuration
	}
	if cfg.Retention <= 0 {
		cfg.Retention = DefaultRetention
	}
	if cfg.ReapInterval <= 0 {
		cfg.ReapInterval = DefaultReapInterval
	}
	if cfg.ReapBatchSize <= 0 {
		cfg.ReapBatchSize = DefaultReapBatchSize
	}

	cfg.Backoff.EnsureDefaults()
}

// ValidateWithContext validates a RelayConfig.
func (cfg *RelayConfig) ValidateWithContext(ctx context.Context) error {
	return validation.ValidateStructWithContext(ctx, cfg,
		validation.Field(&cfg.Dialect, validation.Required, validation.By(func(any) error {
			if !cfg.Dialect.Valid() {
				return platformerrors.Wrapf(dialect.ErrUnsupported, "outbox dialect %q", cfg.Dialect)
			}

			return nil
		})),
		validation.Field(&cfg.ClaimMode, validation.Required, validation.By(func(any) error {
			if !cfg.ClaimMode.Valid() {
				return platformerrors.Wrapf(ErrInvalidClaimMode, "claim mode %q", cfg.ClaimMode)
			}
			if cfg.ClaimMode == ClaimSkipLocked && !cfg.Dialect.SupportsSkipLocked() {
				return platformerrors.Wrapf(ErrInvalidClaimMode, "dialect %q cannot skip locked rows", cfg.Dialect)
			}

			return nil
		})),
		validation.Field(&cfg.BatchSize, validation.Required, validation.Min(1)),
		validation.Field(&cfg.PollInterval, validation.Required, validation.Min(time.Millisecond)),
		validation.Field(&cfg.LeaseDuration, validation.Required, validation.Min(time.Second)),
		validation.Field(&cfg.Retention, validation.Required, validation.Min(time.Minute)),
		validation.Field(&cfg.ReapInterval, validation.Required, validation.Min(time.Second)),
		validation.Field(&cfg.ReapBatchSize, validation.Required, validation.Min(1)),
	)
}

// ErrInvalidClaimMode indicates a claim mode that is unknown, or unsupported by
// the configured dialect.
var ErrInvalidClaimMode = platformerrors.New("invalid outbox claim mode")

// ErrNilDatabaseClient indicates a nil database.Client was passed to NewRelay.
// It wraps errors.ErrNilInputParameter, so a caller may check either.
var ErrNilDatabaseClient = platformerrors.Wrap(platformerrors.ErrNilInputParameter, "nil database client")

// ErrNilPublisherProvider indicates a nil PublisherProvider was passed to
// NewRelay. It wraps errors.ErrNilInputParameter, so a caller may check either.
var ErrNilPublisherProvider = platformerrors.Wrap(platformerrors.ErrNilInputParameter, "nil publisher provider")

// claimedMessage is one row the relay has taken ownership of.
type claimedMessage struct {
	id       string
	topic    string
	key      string
	payload  []byte
	attempts int
}

// Relay moves committed outbox rows onto the broker. It owns a goroutine
// started by Run and stopped by Close.
type Relay struct {
	client   database.Client
	provider messagequeue.PublisherProvider
	clock    clock.Clock
	o11y     observability.Observer
	logger   logging.Logger

	publishers map[string]messagequeue.Publisher

	stop chan struct{}
	done chan struct{}

	publishedCounter   metrics.Int64Counter
	failedCounter      metrics.Int64Counter
	quarantinedCounter metrics.Int64Counter
	reapedCounter      metrics.Int64Counter
	claimErrCounter    metrics.Int64Counter
	backlogGauge       metrics.Int64Gauge
	backlogAgeGauge    metrics.Int64Gauge
	batchHist          metrics.Float64Histogram
	cycleHist          metrics.Float64Histogram
	publishHist        metrics.Float64Histogram

	tracerProvider  tracing.TracerProvider
	metricsProvider metrics.Provider

	cfg RelayConfig

	publishersMu sync.Mutex
	stopOnce     sync.Once
}

// RelayOption configures a Relay.
type RelayOption func(*Relay)

// WithRelayClock swaps the clock driving the poll loop, leases, and backoff.
func WithRelayClock(c clock.Clock) RelayOption {
	return func(r *Relay) {
		if c != nil {
			r.clock = c
		}
	}
}

// WithRelayLogger attaches a logger. The relay reports every publish failure
// and every quarantine through it; without one, a queue that has stopped
// draining is visible only in metrics.
func WithRelayLogger(logger logging.Logger) RelayOption {
	return func(r *Relay) {
		r.logger = logger
	}
}

// WithRelayTracerProvider attaches a tracer provider. Cycles that claim nothing
// are not traced — a root span every poll interval is noise.
func WithRelayTracerProvider(tracerProvider tracing.TracerProvider) RelayOption {
	return func(r *Relay) {
		r.tracerProvider = tracerProvider
	}
}

// WithRelayMetricsProvider attaches a metrics provider.
func WithRelayMetricsProvider(metricsProvider metrics.Provider) RelayOption {
	return func(r *Relay) {
		r.metricsProvider = metricsProvider
	}
}

// NewRelay builds a Relay. It does not start it; call Run.
//
// ctx is used to validate the config and is not retained — Run takes its own.
func NewRelay(ctx context.Context, cfg *RelayConfig, client database.Client, provider messagequeue.PublisherProvider, opts ...RelayOption) (*Relay, error) {
	if cfg == nil {
		return nil, platformerrors.New("nil outbox relay config provided")
	}
	if client == nil {
		return nil, ErrNilDatabaseClient
	}
	if provider == nil {
		return nil, ErrNilPublisherProvider
	}

	cfg.EnsureDefaults()

	if !dialect.ValidIdentifier(cfg.TableName) {
		return nil, platformerrors.Wrapf(dialect.ErrInvalidIdentifier, "outbox table %q", cfg.TableName)
	}

	r := &Relay{
		cfg:        *cfg,
		client:     client,
		provider:   provider,
		clock:      clock.NewClock(),
		publishers: map[string]messagequeue.Publisher{},
		stop:       make(chan struct{}),
		done:       make(chan struct{}),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(r)
		}
	}

	if err := r.cfg.ValidateWithContext(ctx); err != nil {
		return nil, platformerrors.Wrap(err, "validating outbox relay config")
	}

	r.o11y = observability.NewObserver(serviceName, r.logger, r.tracerProvider)
	r.logger = r.o11y.Logger()

	mp := metrics.EnsureMetricsProvider(r.metricsProvider)

	var err error
	if r.publishedCounter, err = mp.NewInt64Counter(fmt.Sprintf("%s_messages_published", serviceName)); err != nil {
		return nil, platformerrors.Wrap(err, "creating messages published counter")
	}
	if r.failedCounter, err = mp.NewInt64Counter(fmt.Sprintf("%s_messages_failed", serviceName)); err != nil {
		return nil, platformerrors.Wrap(err, "creating messages failed counter")
	}
	if r.quarantinedCounter, err = mp.NewInt64Counter(fmt.Sprintf("%s_messages_quarantined", serviceName)); err != nil {
		return nil, platformerrors.Wrap(err, "creating messages quarantined counter")
	}
	if r.reapedCounter, err = mp.NewInt64Counter(fmt.Sprintf("%s_messages_reaped", serviceName)); err != nil {
		return nil, platformerrors.Wrap(err, "creating messages reaped counter")
	}
	if r.claimErrCounter, err = mp.NewInt64Counter(fmt.Sprintf("%s_claim_errors", serviceName)); err != nil {
		return nil, platformerrors.Wrap(err, "creating claim error counter")
	}
	if r.backlogGauge, err = mp.NewInt64Gauge(fmt.Sprintf("%s_backlog_depth", serviceName)); err != nil {
		return nil, platformerrors.Wrap(err, "creating backlog depth gauge")
	}
	if r.backlogAgeGauge, err = mp.NewInt64Gauge(fmt.Sprintf("%s_backlog_age_seconds", serviceName)); err != nil {
		return nil, platformerrors.Wrap(err, "creating backlog age gauge")
	}
	if r.publishHist, err = mp.NewFloat64Histogram(fmt.Sprintf("%s_publish_latency_ms", serviceName)); err != nil {
		return nil, platformerrors.Wrap(err, "creating publish latency histogram")
	}
	if r.batchHist, err = mp.NewFloat64Histogram(fmt.Sprintf("%s_claimed_batch_size", serviceName)); err != nil {
		return nil, platformerrors.Wrap(err, "creating claimed batch size histogram")
	}
	if r.cycleHist, err = mp.NewFloat64Histogram(fmt.Sprintf("%s_cycle_latency_ms", serviceName)); err != nil {
		return nil, platformerrors.Wrap(err, "creating cycle latency histogram")
	}

	return r, nil
}

// Run is the relay loop. Like eventcapture.Recorder.Run it takes no context:
// tied to a server context it would stop draining while requests were still
// committing outbox rows. The owner calls Close after the server has shut
// down.
//
// Run returns only after Close.
func (r *Relay) Run() {
	defer close(r.done)

	ctx := context.Background()

	pollTicker := r.clock.NewTicker(r.cfg.PollInterval)
	defer pollTicker.Stop()

	reapTicker := r.clock.NewTicker(r.cfg.ReapInterval)
	defer reapTicker.Stop()

	for {
		select {
		case <-r.stop:
			// One last cycle, so rows committed just before shutdown are not
			// left sitting until the next process starts.
			r.cycle(ctx)

			return
		case <-pollTicker.Chan():
			r.cycle(ctx)
		case <-reapTicker.Chan():
			r.reap(ctx)
			// Sampled on the reap tick rather than every poll: it is an
			// aggregate over the unpublished rows, and at poll cadence it would
			// cost more than the work it reports on.
			r.sampleBacklog(ctx)
		}
	}
}

// Close stops the relay, waits for the in-flight cycle to finish, and releases
// the publishers. Safe to call more than once.
func (r *Relay) Close(ctx context.Context) error {
	_, op := r.o11y.Begin(ctx)
	defer op.End()

	r.stopOnce.Do(func() { close(r.stop) })

	select {
	case <-r.done:
	case <-ctx.Done():
		return op.Error(ctx.Err(), "waiting for outbox relay to drain")
	}

	r.publishersMu.Lock()
	defer r.publishersMu.Unlock()

	for _, p := range r.publishers {
		p.Stop()
	}
	r.publishers = map[string]messagequeue.Publisher{}

	return nil
}

// cycle claims one batch and publishes it. Errors are logged and counted
// rather than returned: there is no caller to hand them to, and the next cycle
// retries.
func (r *Relay) cycle(ctx context.Context) {
	startTime := time.Now()

	msgs, err := r.claim(ctx)
	if err != nil {
		r.claimErrCounter.Add(ctx, 1)
		r.logger.Error("claiming outbox messages", err)

		return
	}

	if len(msgs) == 0 {
		return
	}

	r.batchHist.Record(ctx, float64(len(msgs)))
	defer func() {
		r.cycleHist.Record(ctx, float64(time.Since(startTime).Milliseconds()))
	}()

	ctx, op := r.o11y.Begin(ctx)
	defer op.End()

	op.Set(claimedKey, len(msgs))

	// Published serially, in created_at order. The claim predicate admits at
	// most one message per partition key per batch, so a failure here can never
	// strand a later message for the same key.
	published := make([]string, 0, len(msgs))
	for i := range msgs {
		if err = r.publish(ctx, &msgs[i]); err != nil {
			r.recordFailure(ctx, &msgs[i], err)

			continue
		}

		published = append(published, msgs[i].id)
		r.publishedCounter.Add(ctx, 1, topicAttr(msgs[i].topic))
	}

	if len(published) == 0 {
		return
	}

	if err = r.markPublished(ctx, published); err != nil {
		// The messages are on the broker but still look unpublished. The next
		// cycle republishes them — this is precisely the at-least-once window
		// the package documentation describes.
		op.Acknowledge(err, "marking outbox messages published")
	}
}

// publish sends one message to its topic. The payload is republished as
// json.RawMessage so the broker receives exactly the bytes a direct Publish of
// the original value would have produced.
//
// It carries its own span: the broker round trip is where a cycle spends its
// time, and a single span over the whole batch cannot say which topic is slow.
func (r *Relay) publish(ctx context.Context, msg *claimedMessage) error {
	ctx, op := r.o11y.Begin(ctx)
	defer op.End()

	op.Set(keys.TopicKey, msg.topic).
		Set(messageIDKey, msg.id).
		Set(attemptsKey, msg.attempts)

	if msg.key != "" {
		op.Set(partitionKeyKey, msg.key)
	}

	startTime := time.Now()
	defer func() {
		r.publishHist.Record(ctx, float64(time.Since(startTime).Milliseconds()), topicAttr(msg.topic))
	}()

	publisher, err := r.publisherFor(ctx, msg.topic)
	if err != nil {
		return op.Error(err, "resolving publisher")
	}

	if err = publisher.Publish(ctx, json.RawMessage(msg.payload)); err != nil {
		return op.Error(err, "publishing outbox message")
	}

	return nil
}

// publisherFor resolves and caches one Publisher per topic.
func (r *Relay) publisherFor(ctx context.Context, topic string) (messagequeue.Publisher, error) {
	r.publishersMu.Lock()
	defer r.publishersMu.Unlock()

	if p, ok := r.publishers[topic]; ok {
		return p, nil
	}

	p, err := r.provider.NewPublisher(ctx, topic)
	if err != nil {
		return nil, platformerrors.Wrapf(err, "building publisher for topic %q", topic)
	}

	r.publishers[topic] = p

	return p, nil
}

// claim selects a batch, leases it, and reads it back — all in one
// transaction, so two relays cannot lease the same rows.
func (r *Relay) claim(ctx context.Context) ([]claimedMessage, error) {
	ctx, op := r.o11y.Begin(ctx)
	defer op.End()

	op.SetValues(map[string]any{
		claimModeKey: string(r.cfg.ClaimMode),
		batchSizeKey: r.cfg.BatchSize,
		"db.system":  string(r.cfg.Dialect),
	})

	var claimed []claimedMessage

	err := r.client.WithTransaction(ctx, func(q database.SQLQueryExecutor) error {
		now := r.clock.Now().UTC()

		selectQuery, selectArgs := buildSelectClaimable(
			r.cfg.Dialect, r.cfg.TableName, now, r.cfg.BatchSize, r.cfg.ClaimMode == ClaimSkipLocked,
		)

		ids, err := scanIDs(ctx, q, selectQuery, selectArgs)
		if err != nil {
			return platformerrors.Wrap(err, "selecting claimable outbox messages")
		}

		if len(ids) == 0 {
			return nil
		}

		claimQuery, claimArgs := buildClaim(r.cfg.Dialect, r.cfg.TableName, ids, now.Add(r.cfg.LeaseDuration))
		if _, err = q.ExecContext(ctx, claimQuery, claimArgs...); err != nil {
			return platformerrors.Wrap(err, "claiming outbox messages")
		}

		fetchQuery, fetchArgs := buildFetch(r.cfg.Dialect, r.cfg.TableName, ids)

		claimed, err = scanMessages(ctx, q, fetchQuery, fetchArgs)
		if err != nil {
			return platformerrors.Wrap(err, "reading claimed outbox messages")
		}

		return nil
	})
	if err != nil {
		return nil, op.Error(err, "claiming outbox batch")
	}

	op.Set(claimedKey, len(claimed))

	return claimed, nil
}

// markPublished retires the rows that made it to the broker.
func (r *Relay) markPublished(ctx context.Context, ids []string) error {
	query, args := buildMarkPublished(r.cfg.Dialect, r.cfg.TableName, ids, r.clock.Now().UTC())

	if _, err := r.client.Writer().ExecContext(ctx, query, args...); err != nil {
		return platformerrors.Wrap(err, "marking outbox messages published")
	}

	return nil
}

// recordFailure releases the lease, schedules the retry, and quarantines the
// message once it has exhausted its attempts. A quarantined message is skipped
// by every future claim, so one permanently broken message cannot block the
// queue behind it.
func (r *Relay) recordFailure(ctx context.Context, msg *claimedMessage, cause error) {
	r.failedCounter.Add(ctx, 1, topicAttr(msg.topic))

	quarantine := uint(msg.attempts) >= r.cfg.Backoff.MaxAttempts

	nextAttempt := r.clock.Now().UTC().Add(r.backoffFor(msg.attempts))

	query, args := buildRecordFailure(
		r.cfg.Dialect, r.cfg.TableName, msg.id, nextAttempt, truncateError(cause), quarantine,
	)

	// The partition key matters here more than anywhere else: a keyed message
	// that is failing is also holding up every later message for that key, so
	// the log has to say which key is stalled.
	logger := r.logger.WithValues(map[string]any{
		messageIDKey:    msg.id,
		keys.TopicKey:   msg.topic,
		partitionKeyKey: msg.key,
		attemptsKey:     msg.attempts,
	})

	if _, err := r.client.Writer().ExecContext(ctx, query, args...); err != nil {
		// The lease still expires on its own, so the message is retried
		// regardless — just later than intended.
		logger.Error("recording outbox publish failure", err)

		return
	}

	if quarantine {
		r.quarantinedCounter.Add(ctx, 1, topicAttr(msg.topic))
		logger.Error("quarantining outbox message after exhausting attempts", cause)

		return
	}

	logger.WithValue("next_attempt", nextAttempt).Info("outbox publish failed, retry scheduled")
}

// sampleBacklog records how far behind the relay is. These two gauges are the
// package's primary health signal: every other instrument is a rate or a
// latency, and none of them can distinguish "publishing steadily" from
// "publishing steadily while falling further behind".
func (r *Relay) sampleBacklog(ctx context.Context) {
	ctx, op := r.o11y.Begin(ctx)
	defer op.End()

	depth, age, err := r.backlog(ctx)
	if err != nil {
		op.Acknowledge(err, "sampling outbox backlog")

		return
	}

	ageSeconds := int64(age.Seconds())

	r.backlogGauge.Record(ctx, depth)
	r.backlogAgeGauge.Record(ctx, ageSeconds)

	op.SetValues(map[string]any{
		backlogDepthKey: depth,
		backlogAgeKey:   ageSeconds,
	})
}

// backlog reads how many messages are waiting and how old the oldest is. Split
// out from sampleBacklog because this is the part with something to get wrong:
// MIN over a timestamp column comes back through three different drivers.
//
// An empty backlog reports an age of zero rather than no age at all, so a
// drained queue actively resets the gauge instead of leaving a stale reading
// on the dashboard.
func (r *Relay) backlog(ctx context.Context) (depth int64, age time.Duration, err error) {
	var oldest any
	if err = r.client.Reader().
		QueryRowContext(ctx, buildBacklog(r.cfg.TableName)).
		Scan(&depth, &oldest); err != nil {
		return 0, 0, platformerrors.Wrap(err, "reading outbox backlog")
	}

	created, ok := coerceTime(oldest)
	if !ok {
		return depth, 0, nil
	}

	if age = r.clock.Since(created.UTC()); age < 0 {
		age = 0
	}

	return depth, age, nil
}

// coerceTime normalizes whatever a driver hands back for MIN(created_at).
//
// It is scanned as `any` rather than sql.NullTime because the drivers disagree.
// pgx and go-sql-driver return a time.Time, but modernc's SQLite driver stores a
// bound time.Time as Go's own String() rendering and an aggregate over that
// column loses the declared DATETIME affinity, so it comes back as a plain
// string that sql.NullTime refuses outright.
//
// A NULL — an empty backlog — reports false, and the caller treats that as an
// age of zero.
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
	// zero the gauge.
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

// reap deletes published rows past the retention window.
func (r *Relay) reap(ctx context.Context) {
	ctx, op := r.o11y.Begin(ctx)
	defer op.End()

	before := r.clock.Now().UTC().Add(-r.cfg.Retention)

	op.Set(retentionCutoffKey, before)

	query, args := buildReap(r.cfg.Dialect, r.cfg.TableName, before, r.cfg.ReapBatchSize)

	res, err := r.client.Writer().ExecContext(ctx, query, args...)
	if err != nil {
		op.Acknowledge(err, "reaping published outbox messages")

		return
	}

	affected, err := res.RowsAffected()
	if err != nil {
		op.Acknowledge(err, "counting reaped outbox messages")

		return
	}

	op.Set(reapedKey, affected)

	if affected > 0 {
		r.reapedCounter.Add(ctx, affected)
		op.Logger().Debug("reaped published outbox messages")
	}
}

// topicAttr labels a measurement with its topic. One Relay serves every topic,
// so without this the counters collapse into a single number and a topic whose
// publisher is broken is invisible beside the ones that are fine. Topics are
// low-cardinality by nature, which is what makes this safe as a metric
// dimension.
func topicAttr(topic string) metric.MeasurementOption {
	return metric.WithAttributes(attribute.String(keys.TopicKey, topic))
}

// backoffFor computes the delay before a message's next attempt.
//
// The schedule comes from retry.DelayFor, so the relay and anything using a
// retry.Policy grow their delays identically from the same Config. What differs
// is everything around it: the wait is persisted as a timestamp rather than
// slept through, so it survives a relay restart, and the jitter is full rather
// than equal — several relays share this table, and spreading their next
// attempts across the whole window is what keeps them from re-colliding on
// every round after one contended claim.
func (r *Relay) backoffFor(attempts int) time.Duration {
	if attempts < 1 {
		attempts = 1
	}

	delay := float64(retry.DelayFor(r.cfg.Backoff, uint(attempts)))

	if r.cfg.Backoff.UseJitter {
		// Full jitter. Not security-sensitive: this only decorrelates retry
		// timing between relays.
		delay *= rand.Float64() //nolint:gosec // jitter, not entropy
	}

	// A floor, because a jittered delay can land arbitrarily close to zero and
	// a message that becomes claimable immediately would spin against the same
	// failure rather than waiting out whatever caused it.
	if delay < float64(time.Millisecond) {
		delay = float64(time.Millisecond)
	}

	return time.Duration(delay)
}

// maxStoredErrorLength bounds what goes into last_error, so a pathological
// driver error cannot bloat the row.
const maxStoredErrorLength = 1024

func truncateError(err error) string {
	if err == nil {
		return ""
	}

	s := err.Error()
	if len(s) > maxStoredErrorLength {
		return s[:maxStoredErrorLength]
	}

	return s
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
			err = platformerrors.Wrap(closeErr, "closing outbox id rows")
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

// scanMessages projects claimed rows. The column list comes from
// messageColumns so the query and this scan cannot drift.
func scanMessages(ctx context.Context, q database.SQLQueryExecutor, query string, args []any) (msgs []claimedMessage, err error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = platformerrors.Wrap(closeErr, "closing outbox message rows")
		}
	}()

	for rows.Next() {
		var (
			msg claimedMessage
			key sql.NullString
		)

		if err = rows.Scan(&msg.id, &msg.topic, &key, &msg.payload, &msg.attempts); err != nil {
			return nil, err
		}

		msg.key = key.String
		msgs = append(msgs, msg)
	}

	return msgs, rows.Err()
}
