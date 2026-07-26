package eventcapture

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/primandproper/platform-go/v7/clock"
	"github.com/primandproper/platform-go/v7/observability/logging"
)

const (
	// DefaultBufferSize caps the in-flight event channel when WithBufferSize
	// is not supplied.
	DefaultBufferSize = 1024
	// DefaultFlushInterval is the flusher tick cadence when WithFlushInterval
	// is not supplied.
	DefaultFlushInterval = 5 * time.Second
)

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
	clock         clock.Clock
	events        chan E
	sink          Sink
	logger        logging.Logger
	stop          chan struct{}
	done          chan struct{}
	observe       func(*E)
	onFlush       func(now time.Time, final bool, emit func(record any))
	transform     func(*E) any
	flushInterval time.Duration
	dropped       atomic.Uint64
	loggedDropped uint64 // flusher-goroutine only: high-water mark already logged
	raw           bool
	stopOnce      sync.Once
}

// Option configures a Recorder.
type Option[E any] func(*Recorder[E])

// WithBufferSize caps the in-flight event channel. A full buffer drops (and
// counts) new events rather than ever blocking a caller.
func WithBufferSize[E any](n int) Option[E] {
	return func(r *Recorder[E]) {
		if n > 0 {
			r.events = make(chan E, n)
		}
	}
}

// WithFlushInterval sets the cadence of the flusher tick.
func WithFlushInterval[E any](d time.Duration) Option[E] {
	return func(r *Recorder[E]) {
		if d > 0 {
			r.flushInterval = d
		}
	}
}

// WithClock swaps the clock driving the flush ticker; tests pass a fake so
// flush timing is deterministic.
func WithClock[E any](c clock.Clock) Option[E] {
	return func(r *Recorder[E]) {
		if c != nil {
			r.clock = c
		}
	}
}

// WithLogger attaches a logger for sink errors and drop reporting.
func WithLogger[E any](logger logging.Logger) Option[E] {
	return func(r *Recorder[E]) {
		r.logger = logging.EnsureLogger(logger)
	}
}

// WithoutRawRecords disables the per-event sink write, for compositions that
// only emit derived records (e.g. aggregate rollups via WithOnFlush).
func WithoutRawRecords[E any]() Option[E] {
	return func(r *Recorder[E]) {
		r.raw = false
	}
}

// WithTransform projects each event into the record written to the sink —
// typically a wire-shaped struct with stable JSON tags — instead of the raw
// *E. It runs in the flusher goroutine, off the hot path.
func WithTransform[E any](fn func(*E) any) Option[E] {
	return func(r *Recorder[E]) {
		r.transform = fn
	}
}

// WithObserver runs fn for every consumed event, in the flusher goroutine.
// This is the composition point for an Aggregator's Observe.
func WithObserver[E any](fn func(*E)) Option[E] {
	return func(r *Recorder[E]) {
		r.observe = fn
	}
}

// WithOnFlush runs fn on every flush tick and once more during the final
// drain (with final set). It runs in the flusher goroutine; emit writes a
// record through the sink with the Recorder's error handling. This is the
// composition point for emitting an Aggregator's completed buckets.
func WithOnFlush[E any](fn func(now time.Time, final bool, emit func(record any))) Option[E] {
	return func(r *Recorder[E]) {
		r.onFlush = fn
	}
}

// NewRecorder builds a Recorder over sink. Start it with `go r.Run()` and
// stop it with Close.
func NewRecorder[E any](sink Sink, opts ...Option[E]) *Recorder[E] {
	r := &Recorder[E]{
		events:        make(chan E, DefaultBufferSize),
		sink:          sink,
		raw:           true,
		flushInterval: DefaultFlushInterval,
		clock:         clock.NewClock(),
		logger:        logging.EnsureLogger(nil),
		stop:          make(chan struct{}),
		done:          make(chan struct{}),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(r)
		}
	}

	return r
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

	ticker := r.clock.NewTicker(r.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case ev := <-r.events:
			r.consume(&ev)
		case <-ticker.Chan():
			r.flush(r.clock.Now(), false)
		case <-r.stop:
			r.drain()

			return
		}
	}
}

// Close stops the flusher and waits for it to drain buffered events and close
// the sink, up to ctx's deadline. Safe to call more than once.
func (r *Recorder[E]) Close(ctx context.Context) error {
	r.stopOnce.Do(func() { close(r.stop) })

	select {
	case <-r.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// consume applies one event to the configured paths. Sink errors are logged,
// never surfaced: the request that produced the event has long been answered.
func (r *Recorder[E]) consume(ev *E) {
	if r.raw {
		record := any(ev)
		if r.transform != nil {
			record = r.transform(ev)
		}
		if record != nil {
			if err := r.sink.Write(record); err != nil {
				r.logger.Error("writing captured event", err)
			}
		}
	}

	if r.observe != nil {
		r.observe(ev)
	}
}

// flush runs the tick hook, reports drop counters, and flushes the sink.
func (r *Recorder[E]) flush(now time.Time, final bool) {
	if r.onFlush != nil {
		r.onFlush(now, final, func(record any) {
			if err := r.sink.Write(record); err != nil {
				r.logger.Error("writing flush-emitted record", err)
			}
		})
	}

	if d := r.dropped.Load(); d > r.loggedDropped {
		r.logger.WithValues(map[string]any{"dropped": d - r.loggedDropped, "total": d}).Info("captured events dropped: buffer full")
		r.loggedDropped = d
	}

	if err := r.sink.Flush(); err != nil {
		r.logger.Error("flushing capture sink", err)
	}
}

// drain empties the channel after stop, then does a final full flush and
// closes the sink. New Record calls racing the drain may still land in the
// buffer and are consumed too; anything sent after the final sweep is dropped
// by the closed sink's error path, not lost silently mid-file.
func (r *Recorder[E]) drain() {
	for {
		select {
		case ev := <-r.events:
			r.consume(&ev)
		default:
			r.flush(r.clock.Now(), true)
			if err := r.sink.Close(); err != nil {
				r.logger.Error("closing capture sink", err)
			}

			return
		}
	}
}
