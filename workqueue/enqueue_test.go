package workqueue

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	platformerrors "github.com/primandproper/platform-go/v10/errors"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// recordingWriter stands in for the upsert, capturing every flush so a test can
// see how the batcher merged what it was given.
type recordingWriter struct {
	// gate, when non-nil, holds each flush open until it is closed. That is how
	// a test makes callers arrive "while a flush is in flight", which is the
	// only condition under which merging happens at all.
	gate chan struct{}

	err error

	batches [][]encodedEntry
	// started counts flushes that have begun, flushes those that have finished.
	// A test that wants a caller to arrive "during a flush" has to wait on the
	// former: the latter cannot tick until the gate opens.
	started atomic.Int64
	flushes atomic.Int64
	mu      sync.Mutex
}

func (w *recordingWriter) write(_ context.Context, rows []encodedEntry) error {
	w.started.Add(1)

	if w.gate != nil {
		<-w.gate
	}

	w.flushes.Add(1)

	w.mu.Lock()
	defer w.mu.Unlock()

	w.batches = append(w.batches, rows)

	return w.err
}

func (w *recordingWriter) rows() [][]encodedEntry {
	w.mu.Lock()
	defer w.mu.Unlock()

	out := make([][]encodedEntry, len(w.batches))
	copy(out, w.batches)

	return out
}

func TestEnqueueBatcher(T *testing.T) {
	T.Parallel()

	T.Run("a lone caller's rows are written", func(t *testing.T) {
		t.Parallel()

		w := &recordingWriter{}
		b := newEnqueueBatcher(w.write)
		t.Cleanup(func() { _ = b.close(t.Context()) })

		must.NoError(t, b.enqueue(t.Context(), []encodedEntry{{key: "a", priority: 3}}))

		batches := w.rows()
		must.SliceLen(t, 1, batches)
		must.SliceLen(t, 1, batches[0])
		test.EqOp(t, "a", batches[0][0].key)
		test.EqOp(t, 3, batches[0][0].priority)
	})

	// The point of the whole type: callers arriving during a flush ride the next
	// one together, so a busy process issues one statement rather than N.
	T.Run("callers arriving during a flush are merged into one", func(t *testing.T) {
		t.Parallel()

		w := &recordingWriter{gate: make(chan struct{})}
		b := newEnqueueBatcher(w.write)

		// The first caller occupies the flusher; everyone after it accumulates.
		first := make(chan error, 1)
		go func() { first <- b.enqueue(context.Background(), []encodedEntry{{key: "first"}}) }()

		waitFor(t, func() bool { return w.started.Load() > 0 })

		var wg sync.WaitGroup
		results := make([]error, 3)

		for i, key := range []string{"a", "b", "c"} {
			wg.Go(func() {
				results[i] = b.enqueue(context.Background(), []encodedEntry{{key: key}})
			})
		}

		// Every merged caller has to be in the open batch before the gate opens,
		// or they would trickle into separate flushes and prove nothing.
		waitFor(t, func() bool { return b.pendingSize() == 3 })

		close(w.gate)
		must.NoError(t, <-first)
		wg.Wait()

		for _, err := range results {
			test.NoError(t, err)
		}

		must.NoError(t, b.close(t.Context()))

		batches := w.rows()
		must.SliceLen(t, 2, batches)
		test.SliceLen(t, 1, batches[0])
		test.SliceLen(t, 3, batches[1])
	})

	// The batch's merge rule has to match the ON CONFLICT clause exactly, or one
	// key's outcome would depend on whether two callers happened to land in the
	// same flush.
	T.Run("duplicate keys collapse to the loudest and soonest", func(t *testing.T) {
		t.Parallel()

		w := &recordingWriter{}
		b := newEnqueueBatcher(w.write)
		t.Cleanup(func() { _ = b.close(t.Context()) })

		must.NoError(t, b.enqueue(t.Context(), []encodedEntry{
			{key: "a", priority: 1, delayMicros: 500},
			{key: "a", priority: 7, delayMicros: 20},
			{key: "a", priority: 3, delayMicros: 900},
		}))

		batches := w.rows()
		must.SliceLen(t, 1, batches)
		must.SliceLen(t, 1, batches[0])
		test.EqOp(t, 7, batches[0][0].priority)
		test.EqOp(t, int64(20), batches[0][0].delayMicros)
	})

	// buildUpsert's lock ordering depends on this, so it is the batcher's job
	// rather than a caller's.
	T.Run("a flushed batch is sorted by key", func(t *testing.T) {
		t.Parallel()

		w := &recordingWriter{}
		b := newEnqueueBatcher(w.write)
		t.Cleanup(func() { _ = b.close(t.Context()) })

		must.NoError(t, b.enqueue(t.Context(), []encodedEntry{
			{key: "c"}, {key: "a"}, {key: "b"},
		}))

		batches := w.rows()
		must.SliceLen(t, 1, batches)
		must.SliceLen(t, 3, batches[0])
		test.EqOp(t, "a", batches[0][0].key)
		test.EqOp(t, "b", batches[0][1].key)
		test.EqOp(t, "c", batches[0][2].key)
	})

	T.Run("a write failure reaches every waiter on that batch", func(t *testing.T) {
		t.Parallel()

		sentinel := platformerrors.New("upsert exploded")
		w := &recordingWriter{err: sentinel}
		b := newEnqueueBatcher(w.write)
		t.Cleanup(func() { _ = b.close(t.Context()) })

		test.ErrorIs(t, b.enqueue(t.Context(), []encodedEntry{{key: "a"}}), sentinel)
	})

	// A caller that gives up must not cancel the flush the rest of the batch is
	// still waiting on.
	T.Run("a caller's expired context does not stop the flush", func(t *testing.T) {
		t.Parallel()

		w := &recordingWriter{gate: make(chan struct{})}
		b := newEnqueueBatcher(w.write)

		ctx, cancel := context.WithCancel(context.Background())

		done := make(chan error, 1)
		go func() { done <- b.enqueue(ctx, []encodedEntry{{key: "abandoned"}}) }()

		waitFor(t, func() bool { return w.started.Load() > 0 })
		cancel()

		test.ErrorIs(t, <-done, context.Canceled)

		close(w.gate)
		must.NoError(t, b.close(t.Context()))

		// The keys landed anyway, which is the right outcome: the work was still
		// worth doing, and its other waiters were still waiting for it.
		test.True(t, w.flushes.Load() > 0)
	})

	T.Run("close flushes what was still accumulating", func(t *testing.T) {
		t.Parallel()

		w := &recordingWriter{}
		b := newEnqueueBatcher(w.write)

		// Written directly into the open batch so that nothing wakes the
		// flusher; close is the only thing that can write it.
		b.join([]encodedEntry{{key: "straggler"}})
		b.drainWake()

		must.NoError(t, b.close(t.Context()))

		batches := w.rows()
		must.SliceLen(t, 1, batches)
		test.EqOp(t, "straggler", batches[0][0].key)
	})

	T.Run("enqueue after close is refused rather than parked", func(t *testing.T) {
		t.Parallel()

		w := &recordingWriter{}
		b := newEnqueueBatcher(w.write)

		must.NoError(t, b.close(t.Context()))

		test.ErrorIs(t, b.enqueue(t.Context(), []encodedEntry{{key: "late"}}), ErrClosed)
	})

	T.Run("close is safe to call more than once", func(t *testing.T) {
		t.Parallel()

		b := newEnqueueBatcher((&recordingWriter{}).write)

		must.NoError(t, b.close(t.Context()))
		must.NoError(t, b.close(t.Context()))
	})
}

func TestMergeEntries(T *testing.T) {
	T.Parallel()

	T.Run("the first entry for a key is taken as-is", func(t *testing.T) {
		t.Parallel()

		merged := mergeEntries(encodedEntry{}, encodedEntry{key: "a", priority: 2, delayMicros: 9})

		test.EqOp(t, "a", merged.key)
		test.EqOp(t, 2, merged.priority)
		test.EqOp(t, int64(9), merged.delayMicros)
	})

	T.Run("priority rises and delay falls", func(t *testing.T) {
		t.Parallel()

		merged := mergeEntries(
			encodedEntry{key: "a", priority: 5, delayMicros: 100},
			encodedEntry{key: "a", priority: 1, delayMicros: 10},
		)

		test.EqOp(t, 5, merged.priority)
		test.EqOp(t, int64(10), merged.delayMicros)
	})
}

func TestSortAndDedupe(T *testing.T) {
	T.Parallel()

	T.Run("sorts and removes repeats", func(t *testing.T) {
		t.Parallel()

		test.Eq(t, []string{"a", "b", "c"}, sortAndDedupe([]string{"c", "a", "b", "a", "c"}))
	})

	T.Run("an empty batch stays empty", func(t *testing.T) {
		t.Parallel()

		test.SliceEmpty(t, sortAndDedupe(nil))
	})
}

// pending exposes the open batch for the tests that have to observe merging as
// it happens.
func (b *enqueueBatcher) pending() *enqueueBatch {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.open
}

// pendingSize reports how many keys the open batch holds.
func (b *enqueueBatcher) pendingSize() int {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.open == nil {
		return 0
	}

	return len(b.open.rows)
}

// drainWake consumes a pending wake token, so a test can put rows in the open
// batch without the flusher picking them up.
func (b *enqueueBatcher) drainWake() {
	select {
	case <-b.wake:
	default:
	}
}

// waitFor polls until cond holds, failing the test rather than hanging if it
// never does.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}

		time.Sleep(time.Millisecond)
	}

	t.Fatal("condition never held")
}
