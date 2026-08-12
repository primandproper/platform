package outboxemit

import (
	"github.com/primandproper/platform-go/v10/clock"
	platformerrors "github.com/primandproper/platform-go/v10/errors"
	"github.com/primandproper/platform-go/v10/observability/logging"
	"github.com/primandproper/platform-go/v10/observability/metrics"
	"github.com/primandproper/platform-go/v10/observability/tracing"
	"github.com/primandproper/platform-go/v10/outbox"
	searchsync "github.com/primandproper/platform-go/v10/search/sync"
)

// registration is one WithSideEffect call. The function is held as an any
// because Option carries no type parameter; NewEmitter narrows it and reports a
// mismatch.
type registration struct {
	fn   any
	name string
}

// options accumulates what an Option sets, so Option does not have to carry the
// type parameter of the thing it configures.
//
// Option is not parameterized on M even though it configures a generic type. Go
// cannot infer a type argument from a call's result type, so an Option[M] would
// force every call site to spell the message type out by hand —
// WithLogger[DataChange](logger) — forever. Only WithSideEffect genuinely needs
// M, and it infers it from its argument.
type options struct {
	logger          logging.Logger
	tracerProvider  tracing.Provider
	metricsProvider metrics.Provider
	clock           clock.Clock
	sideEffects     []registration
}

func newOptions() *options {
	return &options{clock: clock.NewClock()}
}

// Option configures an Emitter. The zero configuration works: an absent logger
// logs nowhere, an absent tracer provider traces nowhere, and an absent metrics
// provider records nothing.
type Option func(*options)

// WithLogger attaches a logger.
func WithLogger(logger logging.Logger) Option {
	return func(o *options) { o.logger = logger }
}

// WithTracerProvider attaches a tracer provider, so an Emit shows up as a child
// of the span that owns the transaction, carrying what it fanned out to.
func WithTracerProvider(tracerProvider tracing.Provider) Option {
	return func(o *options) { o.tracerProvider = tracerProvider }
}

// WithMetricsProvider attaches a metrics provider, enabling outbox_emit_fanout
// beside the request, error and latency trio. Pair it with the Writer's and the
// Relay's — see the package documentation for what each one answers.
func WithMetricsProvider(metricsProvider metrics.Provider) Option {
	return func(o *options) { o.metricsProvider = metricsProvider }
}

// WithClock swaps the clock the emit latency is measured against. Tests
// generally do not need it: under testing/synctest the default clock already
// runs on bubble time.
func WithClock(c clock.Clock) Option {
	return func(o *options) {
		if c != nil {
			o.clock = c
		}
	}
}

// WithSideEffect registers a side effect this module does not own, run on every
// Emit inside the caller's transaction. Webhook dispatch is what it is for; see
// SideEffect.
//
// name identifies it on spans and in the error a failing side effect returns,
// so it must be unique within an Emitter and stable across deploys. Registering
// two under one name is refused rather than resolved by position.
//
// M is inferred from the side effect, so this needs no type argument:
//
//	outboxemit.WithSideEffect("webhooks", dispatchWebhooks)
//
// It must match the Emitter it configures. Because Option carries no type
// parameter, a side effect for the wrong message type cannot be rejected by the
// compiler; NewEmitter returns ErrSideEffectTypeMismatch instead, at
// construction.
//
// Registration is deliberately construction-time and not per-call. A side
// effect every write owes is a property of the wiring, not of the call site —
// if a call site could decline it, the one that forgets to ask for it is back,
// and it is the one nothing afterwards can detect.
func WithSideEffect[M any](name string, fn SideEffect[M]) Option {
	return func(o *options) {
		o.sideEffects = append(o.sideEffects, registration{name: name, fn: fn})
	}
}

// emitPlan is what a call's EmitOptions accumulate before any of it is written.
//
// The first error wins and stops the emission. Options cannot return one — they
// are applied, not called — so the plan carries it to Emit, which is the last
// place that can still refuse before the transaction has written anything.
type emitPlan struct {
	err   error
	key   string
	extra []outbox.Message
}

func (p *emitPlan) fail(err error) {
	if p.err == nil {
		p.err = err
	}
}

// EmitOption adds to one Emit call.
type EmitOption func(*emitPlan)

// WithOrderingKey keys the caller's own message, which is what buys ordering
// for it: the outbox admits a keyed message only when no older message with
// that key is still pending, so at most one message per key is in flight across
// the whole relay fleet, however many relays are running. Messages under
// different keys stay free to interleave.
//
// The key is the ID of whatever the message is about. That is the convention
// worth stating once here rather than at every call site: getting it wrong
// produces reordered writes under relay concurrency, which is a failure that
// appears only under load and only sometimes.
//
// Unkeyed is the default and is not an error; an explicitly empty key is,
// because a caller asking for ordering and receiving none has no way to notice.
func WithOrderingKey(key string) EmitOption {
	return func(p *emitPlan) {
		if key == "" {
			p.fail(ErrEmptyOrderingKey)

			return
		}

		p.key = key
	}
}

// WithIndexUpsert adds a search index event saying the document was created or
// updated, bound for topic.
//
// The event names the document; it does not carry one. Whenever a searchsync
// Syncer applies it, and however many times, it reads the row back and indexes
// its current state — so redelivery and out-of-order delivery both converge,
// and an upsert whose row has since been deleted is applied as a delete rather
// than stranding a document nothing will mention again.
//
// Which index it belongs to is carried by topic, because a searchsync.Event
// holds a document ID and an operation and nothing else. One write touching two
// indexes passes this twice.
//
// The document ID becomes the message's ordering key, independently of
// WithOrderingKey, which keys the caller's own message. That is what buys
// per-document ordering.
func WithIndexUpsert(topic, documentID string) EmitOption {
	return indexEvent(searchsync.OpUpsert, topic, documentID)
}

// WithIndexDelete adds a search index event saying the row is gone, bound for
// topic. The Syncer removes the document without reading anything back; there
// is nothing left to read.
func WithIndexDelete(topic, documentID string) EmitOption {
	return indexEvent(searchsync.OpDelete, topic, documentID)
}

// indexEvent is the shared body of the two index options, so the ordering-key
// convention and the two refusals are stated once.
func indexEvent(op searchsync.Op, topic, documentID string) EmitOption {
	return func(p *emitPlan) {
		if topic == "" {
			p.fail(platformerrors.Wrapf(outbox.ErrEmptyTopic, "index %s event", op))

			return
		}

		if documentID == "" {
			p.fail(platformerrors.Wrapf(ErrEmptyDocumentID, "index %s event for topic %q", op, topic))

			return
		}

		// Event.Message keys the message by document ID, which is where the
		// per-document ordering comes from.
		p.extra = append(p.extra, searchsync.NewEvent(op, documentID).Message(topic))
	}
}

// WithMessages adds outbox messages verbatim, for a side effect that belongs to
// one call rather than to every one.
//
// A side effect every write owes is a WithSideEffect registration instead —
// this is the escape hatch for the other kind, and for a message shape this
// module does not model. The Writer validates them: an empty topic or a nil
// payload fails the emission.
func WithMessages(msgs ...outbox.Message) EmitOption {
	return func(p *emitPlan) {
		p.extra = append(p.extra, msgs...)
	}
}
