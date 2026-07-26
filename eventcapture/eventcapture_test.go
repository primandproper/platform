package eventcapture

import (
	"sync"
	"testing"
	"time"

	clockfake "github.com/primandproper/platform-go/v7/clock/fake"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

type testEvent struct {
	At   time.Time
	Name string
}

// recordingSink is a threadsafe in-memory Sink that signals every Flush, so
// tests can wait for a tick to complete instead of sleeping.
type recordingSink struct {
	flushes chan struct{}
	records []any
	mu      sync.Mutex
	closed  bool
}

func newRecordingSink() *recordingSink {
	return &recordingSink{flushes: make(chan struct{}, 16)}
}

func (s *recordingSink) Write(record any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, record)

	return nil
}

func (s *recordingSink) Flush() error {
	select {
	case s.flushes <- struct{}{}:
	default:
	}

	return nil
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

func TestRecorder_RecordAndClose(T *testing.T) {
	T.Parallel()

	T.Run("events flow to the sink and Close drains and closes", func(t *testing.T) {
		t.Parallel()

		sink := newRecordingSink()
		r := NewRecorder[testEvent](sink, WithClock[testEvent](clockfake.New(testStart)))

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

	T.Run("Close is idempotent", func(t *testing.T) {
		t.Parallel()

		sink := newRecordingSink()
		r := NewRecorder[testEvent](sink, WithClock[testEvent](clockfake.New(testStart)))

		go r.Run()

		must.NoError(t, r.Close(t.Context()))
		must.NoError(t, r.Close(t.Context()))
	})

	T.Run("a full buffer drops and counts instead of blocking", func(t *testing.T) {
		t.Parallel()

		sink := newRecordingSink()
		// No Run(): nothing consumes, so the buffer genuinely fills.
		r := NewRecorder[testEvent](sink,
			WithBufferSize[testEvent](1),
			WithClock[testEvent](clockfake.New(testStart)),
		)

		r.Record(&testEvent{Name: "kept"})
		r.Record(&testEvent{Name: "dropped-a"})
		r.Record(&testEvent{Name: "dropped-b"})

		test.EqOp(t, uint64(2), r.Dropped())
	})

	T.Run("WithoutRawRecords suppresses per-event writes", func(t *testing.T) {
		t.Parallel()

		sink := newRecordingSink()
		var observed int
		r := NewRecorder[testEvent](sink,
			WithClock[testEvent](clockfake.New(testStart)),
			WithoutRawRecords[testEvent](),
			WithObserver[testEvent](func(*testEvent) { observed++ }),
		)

		go r.Run()

		r.Record(&testEvent{Name: "only-observed"})

		must.NoError(t, r.Close(t.Context()))
		must.SliceLen(t, 0, sink.snapshot())
		test.EqOp(t, 1, observed)
	})

	T.Run("WithTransform projects events before the sink", func(t *testing.T) {
		t.Parallel()

		sink := newRecordingSink()
		r := NewRecorder[testEvent](sink,
			WithClock[testEvent](clockfake.New(testStart)),
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
}

func TestRecorder_PeriodicFlush(T *testing.T) {
	T.Parallel()

	T.Run("ticks run the flush hook and flush the sink", func(t *testing.T) {
		t.Parallel()

		sink := newRecordingSink()
		fc := clockfake.New(testStart)

		var hookCalls int
		var sawFinal bool
		var mu sync.Mutex

		r := NewRecorder[testEvent](sink,
			WithClock[testEvent](fc),
			WithFlushInterval[testEvent](time.Second),
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

		// Wait for the flusher's ticker to register before advancing, so the
		// tick lands deterministically.
		fc.BlockUntil(1)
		fc.Advance(time.Second)

		select {
		case <-sink.flushes:
		case <-time.After(5 * time.Second):
			t.Fatal("flush tick never reached the sink")
		}

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
