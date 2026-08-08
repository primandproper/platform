package timers

import (
	"context"
	stderrors "errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// The worker's loop needs a server, and the containers tests drive it there.
// These cover the part that does not: what one pass does with a batch it has
// already been handed — the concurrency limit, the panic containment, and the
// grouping that decides how many Release statements a failed batch costs.

func newTestWorker(t *testing.T, mutate func(*WorkerConfig), handler Handler[string]) *Worker[string] {
	t.Helper()

	set, err := New[string](t.Context(), validConfig(), postgresClient())
	must.NoError(t, err)

	cfg := &WorkerConfig{}
	if mutate != nil {
		mutate(cfg)
	}

	w, err := NewWorker(t.Context(), cfg, set, handler)
	must.NoError(t, err)

	return w
}

// dueBatch builds n firings with distinct keys and one shared instant.
func dueBatch(n int) []Due[string] {
	instant := time.Date(2026, time.August, 21, 9, 0, 0, 0, time.UTC)

	batch := make([]Due[string], 0, n)
	for i := range n {
		batch = append(batch, Due[string]{Key: string(rune('a' + i)), RunAt: instant, Attempts: 1})
	}

	return batch
}

func TestNewWorker(T *testing.T) {
	T.Parallel()

	T.Run("builds a worker and defaults its knobs", func(t *testing.T) {
		t.Parallel()

		w := newTestWorker(t, nil, func(context.Context, Due[string]) error { return nil })

		test.EqOp(t, DefaultWorkerBatch, w.cfg.Batch)
		test.EqOp(t, DefaultWorkerLease, w.cfg.Lease)
	})

	// A worker with nothing to call would claim every timer and mark it fired,
	// which looks exactly like a working deployment right up until somebody asks
	// why no reminders went out.
	T.Run("rejects the inputs it cannot work without", func(t *testing.T) {
		t.Parallel()

		set, err := New[string](t.Context(), validConfig(), postgresClient())
		must.NoError(t, err)

		_, err = NewWorker[string](t.Context(), nil, set, func(context.Context, Due[string]) error { return nil })
		test.True(t, stderrors.Is(err, ErrNilConfig))

		_, err = NewWorker[string](t.Context(), &WorkerConfig{}, nil,
			func(context.Context, Due[string]) error { return nil })
		test.True(t, stderrors.Is(err, ErrNilTimers))

		_, err = NewWorker[string](t.Context(), &WorkerConfig{}, set, nil)
		test.True(t, stderrors.Is(err, ErrNilHandler))
	})
}

func TestWorker_Fire(T *testing.T) {
	T.Parallel()

	T.Run("fires every timer in the batch", func(t *testing.T) {
		t.Parallel()

		var seen atomic.Int64

		w := newTestWorker(t, nil, func(context.Context, Due[string]) error {
			seen.Add(1)

			return nil
		})

		fired, failed := w.fire(t.Context(), dueBatch(5))

		test.EqOp(t, int64(5), seen.Load())
		test.SliceLen(t, 5, fired)
		test.SliceEmpty(t, failed)
	})

	// The lease has to cover a whole batch, which is only arithmetic anybody can
	// do if the concurrency limit is real.
	T.Run("never runs more than Concurrency handlers at once", func(t *testing.T) {
		t.Parallel()

		var (
			mu      sync.Mutex
			running int
			peak    int
		)

		w := newTestWorker(t, func(cfg *WorkerConfig) { cfg.Concurrency = 2 },
			func(context.Context, Due[string]) error {
				mu.Lock()
				running++
				peak = max(peak, running)
				mu.Unlock()

				time.Sleep(time.Millisecond)

				mu.Lock()
				running--
				mu.Unlock()

				return nil
			})

		fired, _ := w.fire(t.Context(), dueBatch(8))

		must.SliceLen(t, 8, fired)
		test.LessEq(t, 2, peak)
	})

	// One bad timer must not take the loop down with it, and the firing has to
	// come back rather than being silently retired.
	T.Run("contains a panicking handler and treats it as a failure", func(t *testing.T) {
		t.Parallel()

		w := newTestWorker(t, nil, func(_ context.Context, due Due[string]) error {
			if due.Key == "b" {
				panic("handler exploded")
			}

			return nil
		})

		fired, failed := w.fire(t.Context(), dueBatch(3))

		test.SliceLen(t, 2, fired)
		must.SliceLen(t, 1, failed)
		must.SliceLen(t, 1, failed[0].due)
		test.EqOp(t, "b", failed[0].due[0].Key)
		test.True(t, stderrors.Is(failed[0].cause, ErrHandlerPanicked))
	})

	// The common failure is one dependency being down and taking the whole batch
	// with it, so a batch of identical timeouts must cost one Release rather
	// than twenty.
	T.Run("groups failures that render the same", func(t *testing.T) {
		t.Parallel()

		w := newTestWorker(t, nil, func(context.Context, Due[string]) error {
			// A fresh value per call, deliberately: grouping by error identity
			// would put each of these in its own release.
			return stderrors.New("upstream unavailable")
		})

		_, failed := w.fire(t.Context(), dueBatch(6))

		must.SliceLen(t, 1, failed)
		test.SliceLen(t, 6, failed[0].due)
	})

	T.Run("keeps distinct failures apart", func(t *testing.T) {
		t.Parallel()

		w := newTestWorker(t, nil, func(_ context.Context, due Due[string]) error {
			return stderrors.New("failed on " + due.Key)
		})

		_, failed := w.fire(t.Context(), dueBatch(3))

		test.SliceLen(t, 3, failed)
	})

	// Cancellation stops handing out new firings but never abandons one already
	// running: the writes that record them run on a detached context, so a clean
	// deploy does not leave a batch to be reclaimed and fired twice.
	T.Run("stops handing out firings once the context is done", func(t *testing.T) {
		t.Parallel()

		var seen atomic.Int64

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		w := newTestWorker(t, nil, func(context.Context, Due[string]) error {
			seen.Add(1)

			return nil
		})

		fired, failed := w.fire(ctx, dueBatch(5))

		test.EqOp(t, int64(0), seen.Load())
		test.SliceEmpty(t, fired)
		test.SliceEmpty(t, failed)
	})
}

// Run's exits. The firing loop itself needs a server and lives in the container
// tests; these are the two ways it stops without ever issuing a statement.
func TestWorker_Run_StopsOnADoneContext(T *testing.T) {
	T.Parallel()

	T.Run("returns before claiming anything when the context is already done", func(t *testing.T) {
		t.Parallel()

		var claimed atomic.Int64

		w := newTestWorker(t, nil, func(context.Context, Due[string]) error {
			claimed.Add(1)

			return nil
		})

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		err := w.Run(ctx)

		test.True(t, stderrors.Is(err, context.Canceled))
		test.EqOp(t, int64(0), claimed.Load())
	})
}

// A worker config that cannot be validated is rejected at construction rather
// than becoming a loop with meaningless timings.
//
// The value has to be positive to get here: EnsureDefaults replaces every
// non-positive knob with its default, so a negative Batch is repaired rather
// than rejected. What survives defaulting is a value that is set, small, and
// meant — a sub-millisecond poll being the one worth refusing.
func TestNewWorker_RejectsAnInvalidConfig(T *testing.T) {
	T.Parallel()

	set, err := New[string](T.Context(), validConfig(), postgresClient())
	must.NoError(T, err)

	_, err = NewWorker(T.Context(), &WorkerConfig{Poll: time.Nanosecond}, set,
		func(context.Context, Due[string]) error { return nil })

	test.Error(T, err)
}

// A pass that cannot even claim reports the failure, and the loop logs it and
// waits rather than returning — a database that is briefly unreachable is an
// outage to ride out, not a reason for a fleet's timers to stop firing.
func TestWorker_RidesOutAFailingDatabase(T *testing.T) {
	T.Parallel()

	sentinel := stderrors.New("connection refused")

	set, err := New[string](T.Context(), validConfig(), failingClient(sentinel))
	must.NoError(T, err)

	w, err := NewWorker(T.Context(), &WorkerConfig{Poll: 10 * time.Millisecond}, set,
		func(context.Context, Due[string]) error { return nil })
	must.NoError(T, err)

	T.Run("a pass surfaces what it could not claim", func(t *testing.T) {
		t.Parallel()

		claimed, passErr := w.pass(t.Context())

		test.EqOp(t, 0, claimed)
		test.True(t, stderrors.Is(passErr, sentinel))
	})

	// Run keeps going through it, and stops only when the context does.
	T.Run("the loop keeps going and stops only on the context", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithTimeout(t.Context(), 250*time.Millisecond)
		defer cancel()

		runErr := w.Run(ctx)

		test.True(t, stderrors.Is(runErr, context.DeadlineExceeded))
	})
}
