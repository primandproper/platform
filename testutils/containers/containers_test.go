package containers

import (
	"context"
	"errors"
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
	"github.com/testcontainers/testcontainers-go"
)

type fakeContainer struct {
	id int
}

// terminateRecorder is a Terminable that records how often it was torn down and
// whether the context it was handed was still live at that point.
type terminateRecorder struct {
	err        error
	calls      int
	ctxWasLive bool
}

func (r *terminateRecorder) Terminate(ctx context.Context, _ ...testcontainers.TerminateOption) error {
	r.calls++
	r.ctxWasLive = ctx.Err() == nil
	return r.err
}

// withRunningTests forces the RUN_CONTAINER_TESTS gate open for the duration of a
// test, so Run's own behavior can be exercised without a Docker daemon. Callers
// must not be parallel: RunningTests is package-level state.
func withRunningTests(tb testing.TB, running bool) {
	tb.Helper()

	original := RunningTests
	tb.Cleanup(func() { RunningTests = original })
	RunningTests = running
}

func TestDefaultRetryConfig(T *testing.T) {
	T.Parallel()

	cfg := DefaultRetryConfig()
	test.EqOp(T, uint(defaultMaxAttempts), cfg.MaxAttempts)
	test.EqOp(T, defaultInitialDelay, cfg.InitialDelay)
	test.False(T, cfg.UseJitter)
}

func TestStartWithRetry(T *testing.T) {
	T.Parallel()

	T.Run("succeeds on first attempt", func(t *testing.T) {
		t.Parallel()

		var calls int
		got, err := StartWithRetry(t.Context(), func(_ context.Context) (*fakeContainer, error) {
			calls++
			return &fakeContainer{id: 1}, nil
		})
		must.NoError(t, err)
		must.NotNil(t, got)
		test.EqOp(t, 1, got.id)
		test.EqOp(t, 1, calls)
	})

	T.Run("retries transient failures then succeeds", func(t *testing.T) {
		t.Parallel()

		var calls int
		got, err := StartWithRetry(t.Context(), func(_ context.Context) (*fakeContainer, error) {
			calls++
			if calls < 3 {
				return nil, errors.New("flaky docker")
			}
			return &fakeContainer{id: calls}, nil
		})
		must.NoError(t, err)
		must.NotNil(t, got)
		test.EqOp(t, 3, calls)
		test.EqOp(t, 3, got.id)
	})

	T.Run("gives up after MaxAttempts and returns last error", func(t *testing.T) {
		t.Parallel()

		var calls int
		boom := errors.New("always broken")
		got, err := StartWithRetry(t.Context(), func(_ context.Context) (*fakeContainer, error) {
			calls++
			return nil, boom
		})
		must.ErrorIs(t, err, boom)
		must.Nil(t, got)
		test.EqOp(t, defaultMaxAttempts, calls)
	})

	T.Run("aborts when context is cancelled", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		var calls int
		_, err := StartWithRetry(ctx, func(_ context.Context) (*fakeContainer, error) {
			calls++
			return nil, errors.New("never reached")
		})
		must.Error(t, err)
		// retry policy exits before invoking the callback when ctx is already done.
		test.EqOp(t, 0, calls)
	})
}

// TestRun is deliberately not parallel: its subtests toggle the package-level
// RunningTests gate so Run can be exercised without a Docker daemon.
//
//nolint:paralleltest // mutates the package-level RunningTests gate; must run serially
func TestRun(T *testing.T) {
	T.Run("hands a live container and context to the closure", func(t *testing.T) { //nolint:paralleltest // mutates the package-level RunningTests gate; must run serially
		withRunningTests(t, true)

		rec := &terminateRecorder{}
		var (
			got    *terminateRecorder
			gotCtx context.Context
		)

		Run(t,
			func(context.Context) (*terminateRecorder, error) { return rec, nil },
			func(ctx context.Context, container *terminateRecorder) {
				got, gotCtx = container, ctx
			},
		)

		must.NotNil(t, got)
		test.EqOp(t, rec, got)
		must.NotNil(t, gotCtx)
		test.NoError(t, gotCtx.Err())
		// Termination is deferred to cleanup, so the container is still live here.
		test.EqOp(t, 0, rec.calls)
	})

	T.Run("terminates exactly once, on a context the test cannot cancel", func(t *testing.T) { //nolint:paralleltest // mutates the package-level RunningTests gate; must run serially
		withRunningTests(t, true)

		rec := &terminateRecorder{}
		// Registered before Run, so it runs after Run's own cleanup (LIFO).
		t.Cleanup(func() {
			test.EqOp(t, 1, rec.calls)
			test.True(t, rec.ctxWasLive)
		})

		Run(t,
			func(context.Context) (*terminateRecorder, error) { return rec, nil },
			func(context.Context, *terminateRecorder) {},
		)
	})

	T.Run("logs rather than fails when termination errors", func(t *testing.T) { //nolint:paralleltest // mutates the package-level RunningTests gate; must run serially
		withRunningTests(t, true)

		rec := &terminateRecorder{err: errors.New("docker went away")}
		t.Cleanup(func() {
			test.EqOp(t, 1, rec.calls)
			test.False(t, t.Failed())
		})

		Run(t,
			func(context.Context) (*terminateRecorder, error) { return rec, nil },
			func(context.Context, *terminateRecorder) {},
		)
	})

	T.Run("terminates even when the closure panics", func(t *testing.T) { //nolint:paralleltest // mutates the package-level RunningTests gate; must run serially
		withRunningTests(t, true)

		rec := &terminateRecorder{}
		t.Cleanup(func() { test.EqOp(t, 1, rec.calls) })

		func() {
			defer func() { _ = recover() }()

			Run(t,
				func(context.Context) (*terminateRecorder, error) { return rec, nil },
				func(context.Context, *terminateRecorder) { panic("boom") },
			)
		}()
	})

	T.Run("skips without starting anything when the gate is closed", func(t *testing.T) { //nolint:paralleltest // mutates the package-level RunningTests gate; must run serially
		withRunningTests(t, false)

		var started, ran bool
		t.Cleanup(func() {
			test.False(t, started)
			test.False(t, ran)
			test.True(t, t.Skipped())
		})

		Run(t,
			func(context.Context) (*terminateRecorder, error) {
				started = true
				return &terminateRecorder{}, nil
			},
			func(context.Context, *terminateRecorder) { ran = true },
		)
	})
}
