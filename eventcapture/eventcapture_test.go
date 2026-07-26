package eventcapture

import (
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

type testEvent struct {
	At   time.Time
	Name string
}

// mustRecorder builds a Recorder, failing the test if its instruments cannot
// be constructed.
func mustRecorder[E any](tb testing.TB, sink Sink, opts ...Option[E]) *Recorder[E] {
	tb.Helper()

	r, err := NewRecorder[E](sink, opts...)
	must.NoError(tb, err)

	return r
}

// recordingSink is a threadsafe in-memory Sink that counts every Flush. Tests
// read the count after a synctest.Wait, which parks the flusher without moving
// the clock — so the count is exactly the flushes that were actually due.
type recordingSink struct {
	records []any
	mu      sync.Mutex
	flushes int
	closed  bool
}

func newRecordingSink() *recordingSink {
	return &recordingSink{}
}

func (s *recordingSink) Write(record any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, record)

	return nil
}

func (s *recordingSink) Flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.flushes++

	return nil
}

func (s *recordingSink) flushCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.flushes
}

func (s *recordingSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true

	return nil
}

func (s *recordingSink) snapshot() []any {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]any, len(s.records))
	copy(out, s.records)

	return out
}

func (s *recordingSink) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.closed
}

var testStart = time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

// flushInterval is the tick cadence the periodic-flush tests configure. Bubble
// time makes it free, so the value is arbitrary — but the tests step to a
// nanosecond either side of it, so they fail if the ticker ignores it.
const flushInterval = time.Second

// The Recorder tests run inside synctest bubbles: the flusher goroutine, the
// event buffer, and the flush ticker all live in the bubble, so the default
// clock rides bubble time and a Run/Close handshake that fails to complete is
// reported as a deadlock instead of hanging on a timeout.

func TestRecorder_RecordAndClose(T *testing.T) {
	T.Parallel()

	T.Run("events flow to the sink and Close drains and closes", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			sink := newRecordingSink()
			r := mustRecorder[testEvent](t, sink)

			go r.Run()

			r.Record(&testEvent{Name: "one"})
			r.Record(&testEvent{Name: "two"})

			must.NoError(t, r.Close(t.Context()))

			records := sink.snapshot()
			must.SliceLen(t, 2, records)
			first, ok := records[0].(*testEvent)
			must.True(t, ok)
			test.EqOp(t, "one", first.Name)
			test.True(t, sink.isClosed())
			test.EqOp(t, uint64(0), r.Dropped())
		})
	})

	T.Run("Close is idempotent", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			sink := newRecordingSink()
			r := mustRecorder[testEvent](t, sink)

			go r.Run()

			must.NoError(t, r.Close(t.Context()))
			must.NoError(t, r.Close(t.Context()))
		})
	})

	T.Run("a full buffer drops and counts instead of blocking", func(t *testing.T) {
		t.Parallel()

		sink := newRecordingSink()
		// No Run(): nothing consumes, so the buffer genuinely fills.
		r := mustRecorder[testEvent](t, sink, WithBufferSize[testEvent](1))

		r.Record(&testEvent{Name: "kept"})
		r.Record(&testEvent{Name: "dropped-a"})
		r.Record(&testEvent{Name: "dropped-b"})

		test.EqOp(t, uint64(2), r.Dropped())
	})

	T.Run("WithoutRawRecords suppresses per-event writes", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			sink := newRecordingSink()
			var observed int
			r := mustRecorder[testEvent](t, sink,
				WithoutRawRecords[testEvent](),
				WithObserver[testEvent](func(*testEvent) { observed++ }),
			)

			go r.Run()

			r.Record(&testEvent{Name: "only-observed"})

			must.NoError(t, r.Close(t.Context()))
			must.SliceLen(t, 0, sink.snapshot())
			test.EqOp(t, 1, observed)
		})
	})

	T.Run("WithTransform projects events before the sink", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			sink := newRecordingSink()
			r := mustRecorder[testEvent](t, sink,
				WithTransform[testEvent](func(ev *testEvent) any {
					return map[string]string{"name": ev.Name}
				}),
			)

			go r.Run()

			r.Record(&testEvent{Name: "projected"})

			must.NoError(t, r.Close(t.Context()))

			records := sink.snapshot()
			must.SliceLen(t, 1, records)
			projected, ok := records[0].(map[string]string)
			must.True(t, ok)
			test.EqOp(t, "projected", projected["name"])
		})
	})
}

func TestRecorder_PeriodicFlush(T *testing.T) {
	T.Parallel()

	T.Run("ticks run the flush hook and flush the sink", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			sink := newRecordingSink()

			var hookCalls int
			var sawFinal bool
			var mu sync.Mutex

			r := mustRecorder[testEvent](t, sink,
				WithFlushInterval[testEvent](flushInterval),
				WithOnFlush[testEvent](func(_ time.Time, final bool, emit func(any)) {
					mu.Lock()
					defer mu.Unlock()
					hookCalls++
					if final {
						sawFinal = true
					} else {
						emit("tick-record")
					}
				}),
			)

			go r.Run()

			// Close is idempotent, so the explicit one below still owns the
			// assertions; this only unwinds the flusher if one of them fails
			// first, turning a deadlocked bubble into a plain failure.
			defer func() { _ = r.Close(t.Context()) }()

			// A nanosecond short of the interval. Wait parks the flusher without
			// moving the clock, so the tick is pinned to its deadline rather than
			// to whenever the bubble next goes idle.
			time.Sleep(flushInterval - time.Nanosecond)
			synctest.Wait()
			must.EqOp(t, 0, sink.flushCount())

			// Crossing the deadline fires exactly one tick.
			time.Sleep(time.Nanosecond)
			synctest.Wait()
			must.EqOp(t, 1, sink.flushCount())

			must.NoError(t, r.Close(t.Context()))

			mu.Lock()
			defer mu.Unlock()
			// One periodic tick plus the final drain flush.
			test.EqOp(t, 2, hookCalls)
			test.True(t, sawFinal)

			records := sink.snapshot()
			must.SliceLen(t, 1, records)
			test.Eq(t, any("tick-record"), records[0])
		})
	})

	T.Run("WithOverflowSource is drained on every flush", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			sink := newRecordingSink()

			var mu sync.Mutex
			var polls int

			r := mustRecorder[testEvent](t, sink,
				WithFlushInterval[testEvent](flushInterval),
				WithOverflowSource[testEvent](func() uint64 {
					mu.Lock()
					defer mu.Unlock()
					polls++

					return 3
				}),
			)

			go r.Run()

			defer func() { _ = r.Close(t.Context()) }()

			time.Sleep(flushInterval - time.Nanosecond)
			synctest.Wait()
			must.EqOp(t, 0, sink.flushCount())

			time.Sleep(time.Nanosecond)
			synctest.Wait()
			must.EqOp(t, 1, sink.flushCount())

			must.NoError(t, r.Close(t.Context()))

			mu.Lock()
			defer mu.Unlock()
			// The periodic tick plus the final drain flush: an Aggregator's
			// overflow is reported and reset on both, so a shutdown never strands
			// the last window's discards.
			test.EqOp(t, 2, polls)
		})
	})
}

func TestAggregator(T *testing.T) {
	T.Parallel()

	type counts struct {
		total int
	}

	cmpKeys := func(a, b string) int {
		switch {
		case a < b:
			return -1
		case a > b:
			return 1
		default:
			return 0
		}
	}

	T.Run("folds observations into time buckets", func(t *testing.T) {
		t.Parallel()

		agg := NewAggregator[string, counts](time.Minute, 0, WithKeyOrder[string, counts](cmpKeys))

		inc := func(c *counts) { c.total++ }
		agg.Observe("a", testStart, inc)
		agg.Observe("a", testStart.Add(30*time.Second), inc) // same bucket
		agg.Observe("a", testStart.Add(90*time.Second), inc) // next bucket
		agg.Observe("b", testStart, inc)

		// Only the first window has closed by 1m30s.
		buckets := agg.Flush(testStart.Add(90*time.Second), false)
		must.SliceLen(t, 2, buckets)
		test.EqOp(t, "a", buckets[0].Key)
		test.EqOp(t, 2, buckets[0].Counts.total)
		test.EqOp(t, "b", buckets[1].Key)
		test.EqOp(t, 1, buckets[1].Counts.total)
		test.EqOp(t, testStart, buckets[0].Start)

		// The drain path emits the still-open bucket too.
		rest := agg.Flush(testStart.Add(90*time.Second), true)
		must.SliceLen(t, 1, rest)
		test.EqOp(t, testStart.Add(time.Minute), rest[0].Start)

		// Everything was removed as it flushed.
		test.SliceLen(t, 0, agg.Flush(testStart.Add(time.Hour), true))
	})

	T.Run("bounded cell map drops and counts overflow", func(t *testing.T) {
		t.Parallel()

		agg := NewAggregator[string, counts](time.Minute, 2)

		inc := func(c *counts) { c.total++ }
		agg.Observe("a", testStart, inc)
		agg.Observe("b", testStart, inc)
		agg.Observe("c", testStart, inc) // over the cap: dropped
		agg.Observe("a", testStart, inc) // existing cell: still folded

		test.EqOp(t, uint64(1), agg.TakeOverflow())
		test.EqOp(t, uint64(0), agg.TakeOverflow())

		buckets := agg.Flush(testStart.Add(time.Hour), true)
		must.SliceLen(t, 2, buckets)
	})
}
