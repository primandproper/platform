package outboxemit

import (
	"context"
	"fmt"

	"github.com/primandproper/platform-go/v10/clock"
	"github.com/primandproper/platform-go/v10/database"
	platformerrors "github.com/primandproper/platform-go/v10/errors"
	"github.com/primandproper/platform-go/v10/observability"
	"github.com/primandproper/platform-go/v10/observability/keys"
	"github.com/primandproper/platform-go/v10/observability/metrics"
	"github.com/primandproper/platform-go/v10/outbox"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// serviceName names this package's loggers, spans, and metrics.
const serviceName = "outbox_emit"

// Observability keys for this package's spans and log fields. Declared once so
// a field set on a span and the same field logged beside it cannot drift, and
// so the outbox_emit. prefix is applied uniformly — an un-namespaced attribute
// name collides with every other component writing to the same trace. The topic
// is not here: it is keys.TopicKey, because a reader correlating an emission
// against a publish is correlating on the topic, not on this package.
const (
	messageCountKey = "outbox_emit.message_count"
	sideEffectsKey  = "outbox_emit.side_effects"
	orderingKeyKey  = "outbox_emit.ordering_key"
)

// Enqueuer is the outbox half of an Emitter. *outbox.Writer satisfies it and is
// what a deployment passes.
//
// It is an interface rather than the concrete Writer because an Emitter's whole
// contract is *which messages one call produces*, and that is a property of the
// argument list handed to Enqueue. A test holding a Writer can only read the
// rows back out of a database and infer; a test holding this reads the messages
// themselves.
type Enqueuer interface {
	Enqueue(ctx context.Context, q database.SQLQueryExecutor, msgs ...outbox.Message) error
}

// The Writer is what a deployment passes, so its conformance is a compile-time
// fact rather than something a wiring site discovers.
var _ Enqueuer = (*outbox.Writer)(nil)

// SideEffect is the seam for a side effect this module does not own.
//
// It runs inside the caller's transaction, on the same executor the Emitter was
// given, so whatever it writes commits or rolls back with the row change that
// occasioned it — which is the entire reason it runs here rather than after.
// It may return further outbox messages; they are enqueued with the rest, in
// the same statement.
//
// Webhook dispatch is the case this exists for: read the endpoints subscribed
// to this event, write a dispatch row per endpoint, return no messages. A side
// effect that only derives messages returns them and ignores q. Either shape is
// one registration, and neither can be forgotten at a call site afterwards.
//
// An error aborts the emission and is returned to the caller, whose transaction
// then rolls back — including the row change and every message this call would
// have enqueued. That is the correct outcome: a write whose side effects cannot
// all be recorded is a write that should not have happened.
type SideEffect[M any] func(ctx context.Context, q database.SQLQueryExecutor, msg M) ([]outbox.Message, error)

// namedSideEffect is one registered side effect, resolved to this Emitter's
// message type. The name is what identifies it on a span, so a transaction that
// fanned out to three side effects is legible from the trace alone.
type namedSideEffect[M any] struct {
	fn   SideEffect[M]
	name string
}

// Emitter turns one call inside a transaction into every message that write
// owes: the caller's own message, the index events it implies, and whatever
// consumer-owned side effects were registered.
//
// It holds no database handle. Every Emit takes the caller's executor, exactly
// as outbox.Writer.Enqueue does, so one Emitter serves every transaction in the
// process.
//
// M is the message type, and is the only genuinely consumer-shaped part of any
// of this — an audit record, a data-change notification, a domain event. This
// package holds no opinion about its contents and never looks inside it; it is
// marshaled by the outbox, which pins JSON.
type Emitter[M any] struct {
	writer Enqueuer
	clock  clock.Clock
	o11y   observability.Observer

	fanout metrics.Int64Histogram

	// measurement is the topic attribute every instrument here carries, built
	// once because an Emitter serves exactly one topic.
	measurement metric.MeasurementOption

	ops *metrics.OperationSet

	topic string

	sideEffects []namedSideEffect[M]
}

// NewEmitter builds an Emitter that sends its messages to topic through
// enqueuer.
//
// topic is the destination of the caller's own message — the data-change
// stream, the audit log — and is required. Index events go to their own topics,
// named at each Emit, because which index a document belongs to is carried by
// the topic and one write can touch several.
//
// M cannot be inferred from the arguments, so it is spelled at the wiring site
// and nowhere else:
//
//	emitter, err := outboxemit.NewEmitter[DataChange]("data_changes", writer,
//	    outboxemit.WithSideEffect("webhooks", dispatchWebhooks))
func NewEmitter[M any](topic string, enqueuer Enqueuer, opts ...Option) (*Emitter[M], error) {
	if topic == "" {
		return nil, outbox.ErrEmptyTopic
	}
	if enqueuer == nil {
		return nil, ErrNilEnqueuer
	}

	o := newOptions()
	for _, opt := range opts {
		if opt != nil {
			opt(o)
		}
	}

	e := &Emitter[M]{
		topic:       topic,
		writer:      enqueuer,
		clock:       o.clock,
		measurement: metric.WithAttributes(attribute.String(keys.TopicKey, topic)),
	}

	// An Emitter serves exactly one topic, so the topic is stated here rather
	// than at each call site. Seeding the observer rather than only the logger
	// is what puts it on the spans as well as the log lines.
	e.o11y = observability.NewObserverWithValues(serviceName, o.logger, o.tracerProvider,
		map[string]any{keys.TopicKey: topic})

	var err error
	if e.sideEffects, err = resolveSideEffects[M](o.sideEffects); err != nil {
		return nil, err
	}

	mp := metrics.EnsureMetricsProvider(o.metricsProvider)

	if e.ops, err = metrics.NewOperationSet(mp, serviceName); err != nil {
		return nil, platformerrors.Wrap(err, "creating emit operation set")
	}
	if e.fanout, err = mp.NewInt64Histogram(fmt.Sprintf("%s_fanout", serviceName)); err != nil {
		return nil, platformerrors.Wrap(err, "creating emit fanout histogram")
	}

	return e, nil
}

// resolveSideEffects narrows each registration to this Emitter's message type.
//
// Option carries no type parameter — see WithSideEffect — so a side effect
// written against the wrong message type cannot be rejected by the compiler.
// It is rejected here instead, at construction, rather than being dropped: a
// silently ignored registration is precisely the forgotten side effect this
// package exists to make impossible.
func resolveSideEffects[M any](registered []registration) ([]namedSideEffect[M], error) {
	if len(registered) == 0 {
		return nil, nil
	}

	resolved := make([]namedSideEffect[M], 0, len(registered))
	seen := make(map[string]struct{}, len(registered))

	for i := range registered {
		r := &registered[i]

		if r.name == "" {
			return nil, ErrEmptySideEffectName
		}

		// Names are the span attribute and are how a failure names its culprit,
		// so two side effects under one name would report as each other.
		if _, duplicate := seen[r.name]; duplicate {
			return nil, platformerrors.Wrapf(ErrDuplicateSideEffect, "side effect %q", r.name)
		}
		seen[r.name] = struct{}{}

		fn, ok := r.fn.(SideEffect[M])
		if !ok {
			return nil, platformerrors.Wrapf(ErrSideEffectTypeMismatch,
				"side effect %q is %T, want SideEffect[%T]", r.name, r.fn, *new(M))
		}

		// A nil func survives the assertion above, because an any holding a
		// typed nil is not a nil any. Left unchecked it panics at the first
		// Emit, in a transaction, rather than at construction.
		if fn == nil {
			return nil, platformerrors.Wrapf(ErrNilSideEffect, "side effect %q", r.name)
		}

		resolved = append(resolved, namedSideEffect[M]{name: r.name, fn: fn})
	}

	return resolved, nil
}

// Topic returns the topic this Emitter sends its own messages to.
func (e *Emitter[M]) Topic() string {
	return e.topic
}

// Emit writes msg and every message it implies into the outbox, using the
// caller's executor, so all of them commit or roll back with whatever else that
// transaction did:
//
//	err := client.WithTransaction(ctx, func(q database.SQLQueryExecutor) error {
//	    if err := updateSetting(ctx, q, setting); err != nil {
//	        return err
//	    }
//
//	    return emitter.Emit(ctx, q, DataChange{Type: "setting.updated", ID: setting.ID},
//	        outboxemit.WithIndexUpsert("settings-index", setting.ID),
//	        outboxemit.WithOrderingKey(setting.ID))
//	})
//
// Every message goes in one Enqueue, which is one round trip: a transaction
// that owes three events should not pay three of them inside a lock. They are
// ordered msg first, then the options in the order given, then the registered
// side effects in registration order — deterministic, so a test can read the
// argument list, though nothing downstream depends on it. What order the broker
// sees them in is the outbox's business, and per-key ordering is what actually
// holds.
//
// An option that cannot be honored — an index event with no document ID — fails
// the whole emission rather than being dropped. There is no partial mode here:
// the point of one call is that its side effects are not individually
// forgettable.
func (e *Emitter[M]) Emit(ctx context.Context, q database.SQLQueryExecutor, msg M, opts ...EmitOption) error {
	ctx, op := e.o11y.Begin(ctx)
	defer op.End()

	e.ops.Attempt(ctx, e.measurement)
	defer op.Time(ctx, e.clock, e.ops.Latency, e.measurement)()

	// Checked before anything runs, because a side effect handed a nil executor
	// would write nothing and say so in whatever way it chose to.
	if q == nil {
		e.ops.Failed(ctx, e.measurement)

		return op.Error(outbox.ErrNilExecutor, "emitting outbox messages")
	}

	var p emitPlan
	for _, opt := range opts {
		if opt != nil {
			opt(&p)
		}
	}

	if p.err != nil {
		e.ops.Failed(ctx, e.measurement)

		return op.Error(p.err, "planning outbox emission")
	}

	if p.key != "" {
		op.Set(orderingKeyKey, p.key)
	}

	msgs := make([]outbox.Message, 0, 1+len(p.extra)+len(e.sideEffects))
	msgs = append(msgs, outbox.Message{Topic: e.topic, Payload: msg, Key: p.key})
	msgs = append(msgs, p.extra...)

	if len(e.sideEffects) > 0 {
		names := make([]string, 0, len(e.sideEffects))

		for _, se := range e.sideEffects {
			derived, err := se.fn(ctx, q, msg)
			if err != nil {
				e.ops.Failed(ctx, e.measurement)

				return op.Error(err, "applying outbox side effect %q", se.name)
			}

			msgs = append(msgs, derived...)
			names = append(names, se.name)
		}

		op.Set(sideEffectsKey, names)
	}

	op.Set(messageCountKey, len(msgs))

	if err := e.writer.Enqueue(ctx, q, msgs...); err != nil {
		e.ops.Failed(ctx, e.measurement)

		return op.Error(err, "enqueuing outbox messages")
	}

	// Recorded after the enqueue succeeds. This is the instrument that says a
	// write still owes what it used to: a call site that stopped asking for its
	// index event shows up as a drop here and nowhere else, since the events it
	// no longer writes are events nothing downstream will miss.
	e.fanout.Record(ctx, int64(len(msgs)), e.measurement)

	return nil
}
