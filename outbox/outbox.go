package outbox

import (
	"context"
	"fmt"
	"time"

	"github.com/primandproper/platform-go/v10/clock"
	"github.com/primandproper/platform-go/v10/database"
	"github.com/primandproper/platform-go/v10/database/ddl"
	"github.com/primandproper/platform-go/v10/database/dialect"
	"github.com/primandproper/platform-go/v10/encoding"
	platformerrors "github.com/primandproper/platform-go/v10/errors"
	"github.com/primandproper/platform-go/v10/identifiers"
	"github.com/primandproper/platform-go/v10/observability"
	"github.com/primandproper/platform-go/v10/observability/keys"
	"github.com/primandproper/platform-go/v10/observability/logging"
	"github.com/primandproper/platform-go/v10/observability/metrics"
	"github.com/primandproper/platform-go/v10/observability/tracing"
)

// tableFor renders the outbox table name under a namespace. It is the one
// place the component segment is spelled, so the Writer, the Relay, and the
// DDL cannot disagree about it.
func tableFor(prefix string) string {
	return ddl.Qualify(prefix) + "outbox_messages"
}

// DefaultTablePrefix is the namespace the outbox table carries when none is
// configured, which is none — rendering outbox_messages.
//
// The outbox_ segment is the schema's, not the caller's: a table always says
// which package created it. Setting a namespace of "ddb" renders
// ddb_outbox_messages, for a database shared between applications.
const DefaultTablePrefix = ""

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

	tracerProvider  tracing.Provider
	metricsProvider metrics.Provider
	dialect         dialect.Dialect
	table           string

	// notifyChannel is empty unless WithWriterNotifyChannel was given one, and
	// an empty one emits nothing: an outbox that has not asked for wakeups runs
	// exactly the SQL it always did inside its callers' transactions.
	notifyChannel string
}

// NewWriter builds a Writer for the given dialect.
func NewWriter(d dialect.Dialect, opts ...WriterOption) (*Writer, error) {
	if !d.Valid() {
		return nil, platformerrors.Wrapf(dialect.ErrUnsupported, "outbox dialect %q", d)
	}

	w := &Writer{
		dialect: d,
		table:   tableFor(DefaultTablePrefix),
		clock:   clock.NewClock(),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(w)
		}
	}

	if !dialect.ValidIdentifier(w.table) {
		return nil, platformerrors.Wrapf(dialect.ErrInvalidIdentifier, "outbox table %q", w.table)
	}

	if w.notifyChannel != "" {
		// Refused rather than ignored. A channel configured against MySQL is a
		// deployment that believes it has millisecond wakeups and has silently
		// been running on the poll interval — which is exactly the
		// working-looking noop this module's constructors exist to prevent.
		if !w.dialect.SupportsNotify() {
			return nil, platformerrors.Wrapf(ErrNotifyUnsupported, "outbox dialect %q", w.dialect)
		}

		if !dialect.ValidIdentifier(w.notifyChannel) {
			return nil, platformerrors.Wrapf(dialect.ErrInvalidIdentifier, "outbox notify channel %q", w.notifyChannel)
		}
	}

	w.o11y = observability.NewObserver(serviceName, w.logger, w.tracerProvider)
	w.logger = w.o11y.Logger()
	w.marshaler = encoding.NewClientEncoder(encoding.ContentTypeJSON, encoding.WithLogger(w.logger), encoding.WithTracerProvider(w.tracerProvider))

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

	// On the caller's executor, so the notification is transactional: Postgres
	// delivers it at commit, which means a woken relay cannot look for the rows
	// before they are visible. That exactness is why this rides the same
	// transaction rather than firing after it.
	//
	// The error is returned rather than swallowed. A failed statement has
	// already aborted the caller's transaction, so there is no "carry on
	// without the wakeup" branch to take.
	if w.notifyChannel != "" {
		op.Set(notifyChannelKey, w.notifyChannel)

		if _, err := q.ExecContext(ctx, dialect.PostgresNotifyStatement, w.notifyChannel); err != nil {
			return op.Error(err, "notifying outbox channel %q", w.notifyChannel)
		}
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
