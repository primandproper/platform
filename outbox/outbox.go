package outbox

import (
	"context"
	"fmt"
	"time"

	"github.com/primandproper/platform-go/v8/clock"
	"github.com/primandproper/platform-go/v8/database"
	"github.com/primandproper/platform-go/v8/encoding"
	platformerrors "github.com/primandproper/platform-go/v8/errors"
	"github.com/primandproper/platform-go/v8/identifiers"
	"github.com/primandproper/platform-go/v8/observability"
	"github.com/primandproper/platform-go/v8/observability/keys"
	"github.com/primandproper/platform-go/v8/observability/logging"
	"github.com/primandproper/platform-go/v8/observability/metrics"
	"github.com/primandproper/platform-go/v8/observability/tracing"
)

// DefaultTableName is the table Enqueue and the Relay operate on when no other
// name is configured.
const DefaultTableName = "outbox_messages"

var (
	// ErrEmptyTopic indicates a Message was enqueued without a topic.
	ErrEmptyTopic = platformerrors.New("empty outbox message topic")
	// ErrNilPayload indicates a Message was enqueued with no payload.
	ErrNilPayload = platformerrors.New("nil outbox message payload")
	// ErrNilExecutor indicates Enqueue was called without a query executor.
	ErrNilExecutor = platformerrors.New("nil query executor")
	// ErrUnsupportedDialect indicates a dialect outside the supported set.
	ErrUnsupportedDialect = platformerrors.New("unsupported outbox dialect")
	// ErrInvalidTableName indicates a table name that is not a plain SQL
	// identifier. Table names are interpolated into queries rather than bound,
	// so they are restricted rather than escaped.
	ErrInvalidTableName = platformerrors.New("invalid outbox table name")
)

// Dialect selects the SQL a Writer and Relay emit. It mirrors the providers
// under database/.
type Dialect string

const (
	// DialectPostgres targets PostgreSQL, which numbers its placeholders and
	// supports SKIP LOCKED.
	DialectPostgres Dialect = "postgres"
	// DialectMySQL targets MySQL 8.0+, which supports SKIP LOCKED.
	DialectMySQL Dialect = "mysql"
	// DialectSQLite targets SQLite, which has no SKIP LOCKED and therefore
	// only supports ClaimLease.
	DialectSQLite Dialect = "sqlite"
)

// Valid reports whether d is a dialect this package can emit SQL for.
func (d Dialect) Valid() bool {
	switch d {
	case DialectPostgres, DialectMySQL, DialectSQLite:
		return true
	default:
		return false
	}
}

// supportsSkipLocked reports whether the dialect can claim with FOR UPDATE
// SKIP LOCKED, which is what allows more than one Relay to run at once.
func (d Dialect) supportsSkipLocked() bool {
	return d == DialectPostgres || d == DialectMySQL
}

// Message is one event awaiting publication.
type Message struct {
	// Payload is marshaled to JSON at enqueue and republished verbatim, so the
	// broker sees exactly what a direct Publish of this value would have sent.
	Payload any
	// Topic names the destination; the Relay resolves one Publisher per topic.
	Topic string
	// Key groups messages that must be published in order relative to one
	// another. Empty means unordered. At most one message per key is ever in
	// flight, so per-key order holds even with several relays running.
	Key string
}

// Writer enqueues messages into the outbox table. It holds no database handle:
// every Enqueue takes the caller's executor, so one Writer serves every
// transaction in the process.
type Writer struct {
	clock  clock.Clock
	o11y   observability.Observer
	logger logging.Logger

	// marshaler is pinned to JSON rather than configurable. The Relay hands
	// these bytes to the publisher inside a json.RawMessage, so any other
	// encoding would be spliced verbatim into a JSON message rather than
	// encoded into one. Held as the narrow encoding.Marshaler because bytes,
	// not a transport, are all this needs.
	marshaler encoding.Marshaler

	enqueuedCounter metrics.Int64Counter

	tracerProvider  tracing.TracerProvider
	metricsProvider metrics.Provider
	dialect         Dialect
	table           string
}

// WriterOption configures a Writer.
type WriterOption func(*Writer)

// WithWriterTableName overrides DefaultTableName. The name must be a plain SQL
// identifier: it is interpolated into the query text, not bound as a
// parameter.
func WithWriterTableName(name string) WriterOption {
	return func(w *Writer) {
		if name != "" {
			w.table = name
		}
	}
}

// WithWriterClock swaps the clock used to stamp created_at and next_attempt.
func WithWriterClock(c clock.Clock) WriterOption {
	return func(w *Writer) {
		if c != nil {
			w.clock = c
		}
	}
}

// WithWriterLogger attaches a logger.
func WithWriterLogger(logger logging.Logger) WriterOption {
	return func(w *Writer) {
		w.logger = logger
	}
}

// WithWriterTracerProvider attaches a tracer provider, so an Enqueue shows up
// as a child of the span that owns the transaction.
func WithWriterTracerProvider(tracerProvider tracing.TracerProvider) WriterOption {
	return func(w *Writer) {
		w.tracerProvider = tracerProvider
	}
}

// WithWriterMetricsProvider attaches a metrics provider, enabling
// outbox_messages_enqueued. Pair it with the Relay's provider: enqueue rate
// against publish rate is what tells you whether the relay is keeping up, and
// neither number answers that alone.
func WithWriterMetricsProvider(metricsProvider metrics.Provider) WriterOption {
	return func(w *Writer) {
		w.metricsProvider = metricsProvider
	}
}

// NewWriter builds a Writer for the given dialect.
func NewWriter(dialect Dialect, opts ...WriterOption) (*Writer, error) {
	if !dialect.Valid() {
		return nil, platformerrors.Wrapf(ErrUnsupportedDialect, "dialect %q", dialect)
	}

	w := &Writer{
		dialect: dialect,
		table:   DefaultTableName,
		clock:   clock.NewClock(),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(w)
		}
	}

	if !validIdentifier(w.table) {
		return nil, platformerrors.Wrapf(ErrInvalidTableName, "table %q", w.table)
	}

	w.o11y = observability.NewObserver(serviceName, w.logger, w.tracerProvider)
	w.logger = w.o11y.Logger()
	w.marshaler = encoding.NewClientEncoder(w.logger, w.tracerProvider, encoding.ContentTypeJSON)

	mp := metrics.EnsureMetricsProvider(w.metricsProvider)

	var err error
	if w.enqueuedCounter, err = mp.NewInt64Counter(fmt.Sprintf("%s_messages_enqueued", serviceName)); err != nil {
		return nil, platformerrors.Wrap(err, "creating messages enqueued counter")
	}

	return w, nil
}

// Enqueue writes messages into the outbox using the caller's executor, so they
// commit or roll back with whatever else that transaction did. Passing several
// messages costs one round trip.
//
// Enqueue is deliberately not variadic-only sugar over a loop: a transaction
// that emits three events should not pay three round trips inside a lock.
func (w *Writer) Enqueue(ctx context.Context, q database.SQLQueryExecutor, msgs ...Message) error {
	ctx, op := w.o11y.Begin(ctx)
	defer op.End()

	if q == nil {
		return op.Error(ErrNilExecutor, "enqueuing outbox messages")
	}

	if len(msgs) == 0 {
		return nil
	}

	op.Set(messageCountKey, len(msgs))

	now := w.clock.Now().UTC()

	// Topics are recorded on the span so a transaction that fans out to several
	// destinations is legible from the trace alone, without joining against the
	// counter.
	topics := make([]string, 0, len(msgs))

	rows := make([]enqueueRow, 0, len(msgs))
	for i := range msgs {
		msg := msgs[i]

		if msg.Topic == "" {
			return op.Error(ErrEmptyTopic, "enqueuing outbox messages")
		}
		if msg.Payload == nil {
			return op.Error(platformerrors.Wrapf(ErrNilPayload, "topic %q", msg.Topic), "enqueuing outbox messages")
		}

		payload, err := w.marshaler.Marshal(ctx, msg.Payload)
		if err != nil {
			return op.Error(err, "marshaling outbox payload for topic %q", msg.Topic)
		}

		rows = append(rows, enqueueRow{
			id:        identifiers.New(),
			topic:     msg.Topic,
			key:       msg.Key,
			payload:   payload,
			createdAt: now,
		})
		topics = append(topics, msg.Topic)
	}

	op.Set(keys.TopicKey, topics)

	query, args := buildInsert(w.dialect, w.table, rows)

	if _, err := q.ExecContext(ctx, query, args...); err != nil {
		return op.Error(err, "inserting outbox messages")
	}

	// Counted after the statement succeeds, but the transaction can still roll
	// back afterwards — so this counts intent to publish, not committed rows.
	// The gap is exactly the rollback rate, and comparing this against
	// outbox_messages_published is how you see it.
	for _, topic := range topics {
		w.enqueuedCounter.Add(ctx, 1, topicAttr(topic))
	}

	return nil
}

// enqueueRow is one row's worth of bound parameters.
type enqueueRow struct {
	createdAt time.Time
	id        string
	topic     string
	key       string
	payload   []byte
}
