package eventcapture

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/primandproper/platform-go/v8/clock"
	platformerrors "github.com/primandproper/platform-go/v8/errors"
	"github.com/primandproper/platform-go/v8/observability"
	"github.com/primandproper/platform-go/v8/observability/logging"
	"github.com/primandproper/platform-go/v8/observability/metrics"
	"github.com/primandproper/platform-go/v8/observability/tracing"
)

const (
	// DefaultBufferSize caps the in-flight event channel when WithBufferSize
	// is not supplied.
	DefaultBufferSize = 1024
	// DefaultFlushInterval is the flusher tick cadence when WithFlushInterval
	// is not supplied.
	DefaultFlushInterval = 5 * time.Second

	// serviceName names the Recorder's logger, span, and metrics.
	serviceName = "eventcapture"
)

// ErrEventTypeMismatch indicates WithTransform or WithObserver was given a
// function for an event type other than the Recorder's. Option carries no type
// parameter, so the compiler cannot catch this; NewRecorder reports it instead.
var ErrEventTypeMismatch = platformerrors.New("function event type does not match recorder type")

// Sink persists captured records. Calls arrive from the Recorder's single
// flusher goroutine, so implementations need no locking for Write and Flush,
// though Close may race a final flush and should guard itself. Write receives
// whatever record types the composition emits — raw events, aggregate
// rollups — and must not retain the value past the call.
type Sink interface {
	Write(record any) error
	// Flush pushes buffered records toward durable storage; the Recorder
	// calls it on every tick so a tail -f of a file sink stays current.
	Flush() error
	Close() error
}

// Recorder is the bridge between a hot path and a Sink: Record is a
// non-blocking bounded-channel send (a full buffer drops the event and counts
// it — capture never slows a request), and a single flusher goroutine (Run)
// consumes the channel, writing raw events and running the configured hooks.
// See the package documentation for the lifecycle rationale.
type Recorder[E any] struct {
	clock           clock.Clock
	events          chan E
	sink            Sink
	o11y            observability.Observer
	logger          logging.Logger
	tracerProvider  tracing.TracerProvider
	metricsProvider metrics.Provider
	stop            chan struct{}
	done            chan struct{}
	observe         func(*E)
	onFlush         func(now time.Time, final bool, emit func(record any))
	transform       func(*E) any
	overflow        func() uint64
	writtenCounter  metrics.Int64Counter
	droppedCounter  metrics.Int64Counter
	overflowCounter metrics.Int64Counter
	errCounter      metrics.Int64Counter
	flushHist       metrics.Float64Histogram
	flushInterval   time.Duration
	dropped         atomic.Uint64
	loggedDropped   uint64 // flusher-goroutine only: high-water mark already reported
	raw             bool
	stopOnce        sync.Once
}

// Option configures a Recorder.
//
// It carries no type parameter even though the Recorder does. Go cannot infer a
// type argument from a call's result type, so an Option[E] would force every
// call site to spell the event type out by hand — WithBufferSize[MyEvent](256) —
// forever. WithTransform and WithObserver are the two options that depend on
// the event type; they stay generic but still need no annotation, because E is
// inferable from the function each is handed.
type Option func(*options)

// options accumulates what the options set, so Option can stay free of the
// Recorder's type parameter.
type options struct {
	clock           clock.Clock
	logger          logging.Logger
	tracerProvider  tracing.TracerProvider
	metricsProvider metrics.Provider

	overflow func() uint64
	onFlush  func(now time.Time, final bool, emit func(record any))

	// transform and observe hold func(*E) any and func(*E) for the E of the
	// Recorder being built. They are typed as any because Option cannot name E;
	// NewRecorder asserts them back and reports a mismatch rather than dropping
	// them.
	transform any
	observe   any

	bufferSize    int
	flushInterval time.Duration
	noRawRecords  bool
}

// WithBufferSize caps the in-flight event channel. A full buffer drops (and
// counts) new events rather than ever blocking a caller.
func WithBufferSize(n int) Option {
	return func(o *options) {
		if n > 0 {
			o.bufferSize = n
		}
	}
}

// WithFlushInterval sets the cadence of the flusher tick.
func WithFlushInterval(d time.Duration) Option {
	return func(o *options) {
		if d > 0 {
			o.flushInterval = d
		}
	}
}

// WithClock swaps the clock driving the flush ticker. Tests generally do not
// need it: under testing/synctest the default clock already runs on bubble
// time.
func WithClock(c clock.Clock) Option {
	return func(o *options) {
		if c != nil {
			o.clock = c
		}
	}
}

// WithLogger attaches a logger for sink errors and drop reporting. It is
// named after the package, so capture lines are attributable in aggregate
// logs.
func WithLogger(logger logging.Logger) Option {
	return func(o *options) {
		o.logger = logger
	}
}

// WithTracerProvider attaches a tracer provider. The flusher deliberately does
// not open a span per flush tick — a root span every few seconds, with no
// caller to parent it to, is noise rather than signal. The tracer is used for
// Close, where the drain is a real, once-per-process operation a shutdown
// trace wants to account for.
func WithTracerProvider(tracerProvider tracing.TracerProvider) Option {
	return func(o *options) {
		o.tracerProvider = tracerProvider
	}
}

// WithMetricsProvider attaches a metrics provider, enabling the
// eventcapture_* instruments. These are the only signal that a capture
// pipeline has broken: per the package contract sink errors are never returned
// to a caller, and dropped events never reach the sink at all.
func WithMetricsProvider(metricsProvider metrics.Provider) Option {
	return func(o *options) {
		o.metricsProvider = metricsProvider
	}
}

// WithOverflowSource registers a function the flusher polls each tick to
// report observations an aggregation dropped for exceeding its key bound —
// pass an Aggregator's TakeOverflow. Without it, a full Aggregator discards
// observations silently, since the Recorder cannot see inside a composition
// whose key and counter types belong to the caller.
func WithOverflowSource(fn func() uint64) Option {
	return func(o *options) {
		o.overflow = fn
	}
}

// WithoutRawRecords disables the per-event sink write, for compositions that
// only emit derived records (e.g. aggregate rollups via WithOnFlush).
func WithoutRawRecords() Option {
	return func(o *options) {
		o.noRawRecords = true
	}
}

// WithTransform projects each event into the record written to the sink —
// typically a wire-shaped struct with stable JSON tags — instead of the raw
// *E. It runs in the flusher goroutine, off the hot path.
//
// E is inferred from the function, so this needs no type argument. It must
// match the Recorder it configures; NewRecorder returns ErrEventTypeMismatch
// otherwise, since Option cannot carry E for the compiler to check.
func WithTransform[E any](fn func(*E) any) Option {
	return func(o *options) {
		if fn != nil {
			o.transform = fn
		}
	}
}

// WithObserver runs fn for every consumed event, in the flusher goroutine.
// This is the composition point for an Aggregator's Observe.
//
// E is inferred from the function, so this needs no type argument. It must
// match the Recorder it configures; NewRecorder returns ErrEventTypeMismatch
// otherwise, since Option cannot carry E for the compiler to check.
func WithObserver[E any](fn func(*E)) Option {
	return func(o *options) {
		if fn != nil {
			o.observe = fn
		}
	}
}

// WithOnFlush runs fn on every flush tick and once more during the final
// drain (with final set). It runs in the flusher goroutine; emit writes a
// record through the sink with the Recorder's error handling. This is the
// composition point for emitting an Aggregator's completed buckets.
func WithOnFlush(fn func(now time.Time, final bool, emit func(record any))) Option {
	return func(o *options) {
		o.onFlush = fn
	}
}

// NewRecorder builds a Recorder over sink. Start it with `go r.Run()` and
// stop it with Close. It returns an error only if the metrics provider cannot
// build the Recorder's instruments.
func NewRecorder[E any](sink Sink, opts ...Option) (*Recorder[E], error) {
	if sink == nil {
		return nil, platformerrors.New("nil sink provided")
	}

	o := &options{
		bufferSize:    DefaultBufferSize,
		flushInterval: DefaultFlushInterval,
		clock:         clock.NewClock(),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(o)
		}
	}

	r := &Recorder[E]{
		events:          make(chan E, o.bufferSize),
		sink:            sink,
		raw:             !o.noRawRecords,
		flushInterval:   o.flushInterval,
		clock:           o.clock,
		logger:          o.logger,
		tracerProvider:  o.tracerProvider,
		metricsProvider: o.metricsProvider,
		overflow:        o.overflow,
		onFlush:         o.onFlush,
		stop:            make(chan struct{}),
		done:            make(chan struct{}),
	}

	// Asserted rather than assumed: Option cannot name E, so this is where a
	// function built for another event type is caught. Dropping it silently
	// would leave a composition that looks wired up and records nothing.
	if o.transform != nil {
		transform, ok := o.transform.(func(*E) any)
		if !ok {
			return nil, platformerrors.Wrapf(
				ErrEventTypeMismatch, "transform is %T, want func(*%T) any", o.transform, *new(E),
			)
		}

		r.transform = transform
	}

	if o.observe != nil {
		observe, ok := o.observe.(func(*E))
		if !ok {
			return nil, platformerrors.Wrapf(
				ErrEventTypeMismatch, "observer is %T, want func(*%T)", o.observe, *new(E),
			)
		}

		r.observe = observe
	}

	r.o11y = observability.NewObserver(serviceName, r.logger, r.tracerProvider)
	r.logger = r.o11y.Logger()

	mp := metrics.EnsureMetricsProvider(r.metricsProvider)

	var err error
	if r.writtenCounter, err = mp.NewInt64Counter(fmt.Sprintf("%s_records_written", serviceName)); err != nil {
		return nil, platformerrors.Wrap(err, "creating records written counter")
	}
	if r.droppedCounter, err = mp.NewInt64Counter(fmt.Sprintf("%s_records_dropped", serviceName)); err != nil {
		return nil, platformerrors.Wrap(err, "creating records dropped counter")
	}
	if r.overflowCounter, err = mp.NewInt64Counter(fmt.Sprintf("%s_aggregation_overflow", serviceName)); err != nil {
		return nil, platformerrors.Wrap(err, "creating aggregation overflow counter")
	}
	if r.errCounter, err = mp.NewInt64Counter(fmt.Sprintf("%s_sink_errors", serviceName)); err != nil {
		return nil, platformerrors.Wrap(err, "creating sink error counter")
	}
	if r.flushHist, err = mp.NewFloat64Histogram(fmt.Sprintf("%s_flush_latency_ms", serviceName)); err != nil {
		return nil, platformerrors.Wrap(err, "creating flush latency histogram")
	}

	return r, nil
}

// Record hands one event to the flusher. It never blocks: when the buffer is
// full the event is dropped and counted instead. The event is copied into the
// buffer; the pointer is not retained.
func (r *Recorder[E]) Record(ev *E) {
	select {
	case r.events <- *ev:
	default:
		r.dropped.Add(1)
	}
}

// Dropped reports how many events have been dropped because the buffer was
// full.
func (r *Recorder[E]) Dropped() uint64 {
	return r.dropped.Load()
}

// Run is the flusher loop: it consumes events, ticks the periodic flush, and
// on Close drains the buffer, flushes everything, and closes the sink. Run
// returns only after Close is called.
func (r *Recorder[E]) Run() {
	defer close(r.done)

	// Run deliberately takes no context (see the package documentation), but
	// the instruments need one. Background is the honest choice: the flusher
	// outlives every request whose events it is writing, so there is no
	// caller's context these measurements belong to.
	ctx := context.Background()

	ticker := r.clock.NewTicker(r.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case ev := <-r.events:
			r.consume(ctx, &ev)
		case <-ticker.Chan():
			r.flush(ctx, r.clock.Now(), false)
		case <-r.stop:
			r.drain(ctx)

			return
		}
	}
}

// Close stops the flusher and waits for it to drain buffered events and close
// the sink, up to ctx's deadline. Safe to call more than once.
//
// This is the one traced operation in the package: the drain is a real,
// once-per-process step that a shutdown trace wants accounted for, and a
// deadline hit here means captured events were abandoned.
func (r *Recorder[E]) Close(ctx context.Context) error {
	ctx, op := r.o11y.Begin(ctx)
	defer op.End()

	r.stopOnce.Do(func() { close(r.stop) })

	select {
	case <-r.done:
		return nil
	case <-ctx.Done():
		return op.Error(ctx.Err(), "draining capture buffer before close")
	}
}

// consume applies one event to the configured paths. Sink errors are logged
// and counted, never surfaced: the request that produced the event has long
// been answered.
func (r *Recorder[E]) consume(ctx context.Context, ev *E) {
	if r.raw {
		record := any(ev)
		if r.transform != nil {
			record = r.transform(ev)
		}
		if record != nil {
			r.write(ctx, record, "writing captured event")
		}
	}

	if r.observe != nil {
		r.observe(ev)
	}
}

// write pushes one record through the sink, counting the outcome either way.
func (r *Recorder[E]) write(ctx context.Context, record any, description string) {
	if err := r.sink.Write(record); err != nil {
		r.errCounter.Add(ctx, 1)
		r.logger.Error(description, err)

		return
	}

	r.writtenCounter.Add(ctx, 1)
}

// flush runs the tick hook, reports drop and overflow counters, and flushes
// the sink.
func (r *Recorder[E]) flush(ctx context.Context, now time.Time, final bool) {
	startTime := time.Now()
	defer func() {
		r.flushHist.Record(ctx, float64(time.Since(startTime).Milliseconds()))
	}()

	if r.onFlush != nil {
		r.onFlush(now, final, func(record any) {
			r.write(ctx, record, "writing flush-emitted record")
		})
	}

	// Drops are counted on the hot path with an atomic and reported here, so
	// Record never pays for an instrument call.
	if d := r.dropped.Load(); d > r.loggedDropped {
		delta := d - r.loggedDropped
		r.droppedCounter.Add(ctx, int64(delta))
		r.logger.WithValues(map[string]any{"dropped": delta, "total": d}).Info("captured events dropped: buffer full")
		r.loggedDropped = d
	}

	if r.overflow != nil {
		if ov := r.overflow(); ov > 0 {
			r.overflowCounter.Add(ctx, int64(ov))
			r.logger.WithValue("overflow", ov).Info("aggregation observations dropped: key bound reached")
		}
	}

	if err := r.sink.Flush(); err != nil {
		r.errCounter.Add(ctx, 1)
		r.logger.Error("flushing capture sink", err)
	}
}

// drain empties the channel after stop, then does a final full flush and
// closes the sink. New Record calls racing the drain may still land in the
// buffer and are consumed too; anything sent after the final sweep is dropped
// by the closed sink's error path, not lost silently mid-file.
func (r *Recorder[E]) drain(ctx context.Context) {
	for {
		select {
		case ev := <-r.events:
			r.consume(ctx, &ev)
		default:
			r.flush(ctx, r.clock.Now(), true)
			if err := r.sink.Close(); err != nil {
				r.errCounter.Add(ctx, 1)
				r.logger.Error("closing capture sink", err)
			}

			return
		}
	}
}
