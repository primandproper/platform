package distributedlock_test

import (
	"context"
	"errors"
	"testing"
	"time"

	clockfake "github.com/primandproper/platform-go/v7/clock/fake"
	"github.com/primandproper/platform-go/v7/distributedlock"
	"github.com/primandproper/platform-go/v7/distributedlock/memory"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

const scopedTestKey = "scoped-test"

// newScopedFixture wires the generic adapter around a real memory locker with
// a fake clock, so contention polling is driven by explicit Advance calls.
func newScopedFixture(t *testing.T) (distributedlock.ScopedLocker, distributedlock.Locker, *clockfake.Clock) {
	t.Helper()

	raw, err := memory.NewLocker(nil, nil, nil)
	must.NoError(t, err)

	fc := clockfake.New(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC))

	scoped, err := distributedlock.NewScopedLocker(
		raw,
		distributedlock.WithScopedClock(fc),
		distributedlock.WithScopedLockTTL(time.Minute),
		distributedlock.WithScopedPollInterval(100*time.Millisecond),
	)
	must.NoError(t, err)

	return scoped, raw, fc
}

func TestNewScopedLocker(T *testing.T) {
	T.Parallel()

	T.Run("rejects a nil locker", func(t *testing.T) {
		t.Parallel()

		_, err := distributedlock.NewScopedLocker(nil)
		test.Error(t, err)
	})
}

func TestScopedLocker_TryWithLock(T *testing.T) {
	T.Parallel()

	T.Run("runs fn under the lock and releases after", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		scoped, raw, _ := newScopedFixture(t)

		ran := false
		acquired, err := scoped.TryWithLock(ctx, scopedTestKey, func(context.Context) error {
			ran = true

			// The lock is genuinely held while fn runs.
			_, acquireErr := raw.Acquire(ctx, scopedTestKey, time.Minute)
			test.ErrorIs(t, acquireErr, distributedlock.ErrLockNotAcquired)

			return nil
		})

		must.NoError(t, err)
		test.True(t, acquired)
		test.True(t, ran)

		// And released once fn returns.
		held, err := raw.Acquire(ctx, scopedTestKey, time.Minute)
		must.NoError(t, err)
		must.NoError(t, held.Release(ctx))
	})

	T.Run("contended lock reports false without running fn", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		scoped, raw, _ := newScopedFixture(t)

		held, err := raw.Acquire(ctx, scopedTestKey, time.Minute)
		must.NoError(t, err)
		t.Cleanup(func() { _ = held.Release(ctx) })

		acquired, err := scoped.TryWithLock(ctx, scopedTestKey, func(context.Context) error {
			t.Fatal("fn must not run when the lock is contended")
			return nil
		})

		must.NoError(t, err)
		test.False(t, acquired)
	})

	T.Run("fn errors pass through and the lock is still released", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		scoped, raw, _ := newScopedFixture(t)
		boom := errors.New("boom")

		acquired, err := scoped.TryWithLock(ctx, scopedTestKey, func(context.Context) error {
			return boom
		})

		test.True(t, acquired)
		test.ErrorIs(t, err, boom)

		held, err := raw.Acquire(ctx, scopedTestKey, time.Minute)
		must.NoError(t, err)
		must.NoError(t, held.Release(ctx))
	})

	T.Run("a panicking fn releases the lock and the panic propagates", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		scoped, raw, _ := newScopedFixture(t)

		func() {
			defer func() {
				must.NotNil(t, recover())
			}()

			_, _ = scoped.TryWithLock(ctx, scopedTestKey, func(context.Context) error {
				panic("kaboom")
			})
		}()

		held, err := raw.Acquire(ctx, scopedTestKey, time.Minute)
		must.NoError(t, err)
		must.NoError(t, held.Release(ctx))
	})
}

func TestScopedLocker_WithLock(T *testing.T) {
	T.Parallel()

	T.Run("acquires immediately when uncontended", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		scoped, _, _ := newScopedFixture(t)

		ran := false
		err := scoped.WithLock(ctx, scopedTestKey, func(context.Context) error {
			ran = true
			return nil
		})

		must.NoError(t, err)
		test.True(t, ran)
	})

	T.Run("waits out contention by polling", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		scoped, raw, fc := newScopedFixture(t)

		held, err := raw.Acquire(ctx, scopedTestKey, time.Minute)
		must.NoError(t, err)

		done := make(chan error, 1)
		go func() {
			done <- scoped.WithLock(ctx, scopedTestKey, func(context.Context) error {
				return nil
			})
		}()

		// The adapter is now asleep between polls. Release the lock, then let
		// the next poll fire.
		fc.BlockUntil(1)
		must.NoError(t, held.Release(ctx))
		fc.Advance(100 * time.Millisecond)

		select {
		case err = <-done:
			must.NoError(t, err)
		case <-time.After(5 * time.Second):
			t.Fatal("WithLock did not acquire after the lock was released")
		}
	})

	T.Run("a canceled context ends the wait", func(t *testing.T) {
		t.Parallel()

		scoped, raw, fc := newScopedFixture(t)

		held, err := raw.Acquire(t.Context(), scopedTestKey, time.Minute)
		must.NoError(t, err)
		t.Cleanup(func() { _ = held.Release(t.Context()) })

		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)
		go func() {
			done <- scoped.WithLock(ctx, scopedTestKey, func(context.Context) error {
				t.Error("fn must not run; the lock is never released")
				return nil
			})
		}()

		fc.BlockUntil(1)
		cancel()

		select {
		case err = <-done:
			test.ErrorIs(t, err, context.Canceled)
		case <-time.After(5 * time.Second):
			t.Fatal("WithLock did not observe cancellation")
		}
	})
}
