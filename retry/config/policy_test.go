package retrycfg

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/primandproper/platform-go/v9/retry"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestExponentialBackoffPolicy_Execute(T *testing.T) {
	T.Parallel()

	T.Run("success on first attempt", func(t *testing.T) {
		t.Parallel()

		policy := NewExponentialBackoffPolicy(Config{MaxAttempts: 3})
		ctx := context.Background()
		attempts := 0

		err := policy.Execute(ctx, func(ctx context.Context) error {
			attempts++
			return nil
		})

		must.NoError(t, err)
		test.EqOp(t, 1, attempts)
	})

	T.Run("success after retries", func(t *testing.T) {
		t.Parallel()

		policy := NewExponentialBackoffPolicy(Config{
			MaxAttempts:  5,
			InitialDelay: 1,
			MaxDelay:     10,
			UseJitter:    false,
		})
		ctx := context.Background()
		attempts := 0

		err := policy.Execute(ctx, func(ctx context.Context) error {
			attempts++
			if attempts < 3 {
				return errors.New("transient")
			}
			return nil
		})

		must.NoError(t, err)
		test.EqOp(t, 3, attempts)
	})

	T.Run("returns last error after max attempts", func(t *testing.T) {
		t.Parallel()

		policy := NewExponentialBackoffPolicy(Config{
			MaxAttempts:  3,
			InitialDelay: 1,
			MaxDelay:     10,
			UseJitter:    false,
		})
		ctx := context.Background()
		attempts := 0
		expectedErr := errors.New("final failure")

		err := policy.Execute(ctx, func(ctx context.Context) error {
			attempts++
			if attempts < 3 {
				return errors.New("transient")
			}
			return expectedErr
		})

		must.Error(t, err)
		test.ErrorIs(t, err, expectedErr)
		test.EqOp(t, 3, attempts)
	})

	T.Run("stops immediately when the loop's own context is canceled", func(t *testing.T) {
		t.Parallel()

		policy := NewExponentialBackoffPolicy(Config{
			MaxAttempts:  5,
			InitialDelay: time.Millisecond,
			MaxDelay:     10 * time.Millisecond,
		})
		attempts := 0

		ctx, cancel := context.WithCancel(t.Context())

		err := policy.Execute(ctx, func(context.Context) error {
			attempts++
			cancel()

			return context.Canceled
		})

		test.ErrorIs(t, err, context.Canceled)
		// Retrying under a canceled context is pointless; it must not burn all 5.
		test.EqOp(t, 1, attempts)
	})

	// The regression: a per-attempt timeout is the classic transient failure, and
	// matching the returned error against context.DeadlineExceeded made the loop
	// give up on attempt one for exactly the case it exists to survive.
	T.Run("retries a per-attempt deadline while the loop's context is live", func(t *testing.T) {
		t.Parallel()

		policy := NewExponentialBackoffPolicy(Config{
			MaxAttempts:  5,
			InitialDelay: time.Millisecond,
			MaxDelay:     10 * time.Millisecond,
		})
		attempts := 0

		err := policy.Execute(t.Context(), func(ctx context.Context) error {
			attempts++

			// What an operation that bounds itself with its own timeout returns.
			attemptCtx, cancel := context.WithTimeout(ctx, time.Nanosecond)
			defer cancel()
			<-attemptCtx.Done()

			if attempts < 3 {
				return attemptCtx.Err()
			}

			return nil
		})

		test.NoError(t, err)
		test.EqOp(t, 3, attempts)
	})

	T.Run("stops immediately on an Unretryable error", func(t *testing.T) {
		t.Parallel()

		policy := NewExponentialBackoffPolicy(Config{
			MaxAttempts:  5,
			InitialDelay: time.Millisecond,
			MaxDelay:     10 * time.Millisecond,
		})
		attempts := 0
		underlying := errors.New("fatal")

		err := policy.Execute(context.Background(), func(ctx context.Context) error {
			attempts++
			return retry.Unretryable(underlying)
		})

		test.ErrorIs(t, err, retry.ErrUnretryable)
		test.ErrorIs(t, err, underlying)
		test.EqOp(t, 1, attempts)
	})

	// Regression: a sub-2ns InitialDelay makes int64(delay)/2 truncate to 0, and
	// rand.Int64N(0) panics. With jitter enabled this would crash on the first
	// backoff instead of retrying.
	T.Run("does not panic when jitter delay is too small to halve", func(t *testing.T) {
		t.Parallel()

		policy := NewExponentialBackoffPolicy(Config{
			MaxAttempts:  3,
			InitialDelay: 1,
			MaxDelay:     10,
			UseJitter:    true,
		})
		ctx := context.Background()
		attempts := 0

		err := policy.Execute(ctx, func(ctx context.Context) error {
			attempts++
			return errors.New("transient")
		})

		must.Error(t, err)
		test.EqOp(t, 3, attempts)
	})

	T.Run("respects context cancellation", func(t *testing.T) {
		t.Parallel()

		policy := NewExponentialBackoffPolicy(Config{
			MaxAttempts:  10,
			InitialDelay: time.Hour,
			UseJitter:    false,
		})
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := policy.Execute(ctx, func(ctx context.Context) error {
			return errors.New("fail")
		})

		must.Error(t, err)
		test.ErrorIs(t, err, context.Canceled)
	})
}

func TestDelayFor(T *testing.T) {
	T.Parallel()

	// The schedule this returns is the one Execute sleeps through and the one
	// outbox's relay persists as a timestamp, so these numbers are a contract
	// between two packages rather than an internal detail of either.
	T.Run("grows by the multiplier from a 1-indexed attempt", func(t *testing.T) {
		t.Parallel()

		cfg := Config{InitialDelay: 100 * time.Millisecond, Multiplier: 2, MaxDelay: time.Hour}

		test.EqOp(t, 100*time.Millisecond, DelayFor(cfg, 1))
		test.EqOp(t, 200*time.Millisecond, DelayFor(cfg, 2))
		test.EqOp(t, 400*time.Millisecond, DelayFor(cfg, 3))
		test.EqOp(t, 800*time.Millisecond, DelayFor(cfg, 4))
	})

	T.Run("caps at MaxDelay rather than growing without bound", func(t *testing.T) {
		t.Parallel()

		cfg := Config{InitialDelay: time.Second, Multiplier: 10, MaxDelay: 30 * time.Second}

		test.EqOp(t, 10*time.Second, DelayFor(cfg, 2))
		test.EqOp(t, 30*time.Second, DelayFor(cfg, 3))
		// Far enough out that the unclamped value would overflow a Duration.
		test.EqOp(t, 30*time.Second, DelayFor(cfg, 500))
	})

	// Attempt counts arrive from stored rows and from loop indices, so a zero
	// is a plausible input rather than a programming error. Treating it as the
	// first attempt beats returning zero, which would schedule an immediate
	// retry of something that just failed.
	T.Run("treats an attempt below one as the first attempt", func(t *testing.T) {
		t.Parallel()

		cfg := Config{InitialDelay: 250 * time.Millisecond, Multiplier: 2, MaxDelay: time.Minute}

		test.EqOp(t, DelayFor(cfg, 1), DelayFor(cfg, 0))
	})

	T.Run("does not mutate the config it is given", func(t *testing.T) {
		t.Parallel()

		cfg := Config{InitialDelay: 100 * time.Millisecond, Multiplier: 2, MaxDelay: time.Hour}
		before := cfg

		DelayFor(cfg, 5)

		test.EqOp(t, before, cfg)
	})
}
