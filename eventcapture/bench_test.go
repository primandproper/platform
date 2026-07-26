package eventcapture

import (
	"context"
	"testing"
	"time"
)

// benchSink is a zero-cost Sink. recordingSink appends under a mutex, which
// would dominate the flusher and distort the producer-side measurement, so the
// benchmarks use this instead.
type benchSink struct{}

func (benchSink) Write(any) error { return nil }
func (benchSink) Flush() error    { return nil }
func (benchSink) Close() error    { return nil }

// BenchmarkRecorder_Record measures the only Recorder method that runs on a
// caller's hot path. Record is documented as never blocking a request, so both
// of its outcomes are measured: the buffered send and the buffer-full drop.
// The parallel variant is the shape that actually matters — every request
// handler in a process shares one Recorder, so channel contention, not the
// send itself, is what a capture-enabled service pays.
func BenchmarkRecorder_Record(b *testing.B) {
	ev := &testEvent{At: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), Name: "bench"}

	// newRunning returns a Recorder with a live flusher. The flush interval is
	// pushed past any plausible benchmark duration so a tick never lands mid
	// measurement. A producer that outruns the flusher will still drop events;
	// that is the designed behavior, and the steady state being measured.
	newRunning := func(b *testing.B) *Recorder[testEvent] {
		b.Helper()

		r := mustRecorder[testEvent](
			b,
			benchSink{},
			WithBufferSize[testEvent](8192),
			WithFlushInterval[testEvent](time.Hour),
		)

		go r.Run()
		b.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			_ = r.Close(ctx)
		})

		return r
	}

	b.Run("buffered", func(b *testing.B) {
		r := newRunning(b)

		for b.Loop() {
			r.Record(ev)
		}
	})

	b.Run("buffered-parallel", func(b *testing.B) {
		r := newRunning(b)

		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				r.Record(ev)
			}
		})
	})

	b.Run("full", func(b *testing.B) {
		// No flusher, buffer of one: the first Record fills it and every
		// subsequent one takes the drop path.
		r := mustRecorder[testEvent](b, benchSink{}, WithBufferSize[testEvent](1))
		r.Record(ev)

		for b.Loop() {
			r.Record(ev)
		}
	})
}

// BenchmarkAggregator_Observe covers the per-event fold. Flush is deliberately
// absent: it runs once per flush interval (five seconds by default), so its
// O(n log n) sort is not on any path worth a number here.
func BenchmarkAggregator_Observe(b *testing.B) {
	at := time.Date(2026, time.January, 1, 0, 0, 30, 0, time.UTC)
	fold := func(c *int64) { *c++ }

	b.Run("hit", func(b *testing.B) {
		a := NewAggregator[string, int64](time.Minute, 0)

		for b.Loop() {
			a.Observe("bench-key", at, fold)
		}
	})

	b.Run("overflow", func(b *testing.B) {
		// One cell allowed, already taken: every observation below is for a
		// key that cannot be admitted and is counted instead.
		a := NewAggregator[string, int64](time.Minute, 1)
		a.Observe("seed-key", at, fold)

		for b.Loop() {
			a.Observe("bench-key", at, fold)
		}
	})
}
