package containers

import (
	"context"
	"errors"
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

type fakeContainer struct {
	id int
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
