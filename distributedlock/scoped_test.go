package distributedlock_test

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/primandproper/platform-go/v7/distributedlock"
	"github.com/primandproper/platform-go/v7/distributedlock/memory"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

const (
	scopedTestKey = "scoped-test"
	scopedLockTTL = time.Minute

	// scopedPollInterval is the contention poll cadence the fixture configures.
	// Bubble time makes it free, so the value is arbitrary — but the contention
	// test steps to either side of it, so it fails if polling ignores it.
	scopedPollInterval = 100 * time.Millisecond
)

// noJitter pins the jitter draw to its maximum, so a contended wait is exactly
// the current poll interval. Without it the adapter sleeps somewhere in
// [interval/2, interval] and the tests below could not name a deadline.
func noJitter() float64 { return 1 }

// newScopedFixture wires the generic adapter around a real memory locker. The
// contention tests build it inside a synctest bubble, where the production
// clock reads bubble time, so WithLock's poll sleeps cost no wall time.
func newScopedFixture(t *testing.T, opts ...distributedlock.ScopedOption) (distributedlock.ScopedLocker, distributedlock.Locker) {
	t.Helper()

	raw, err := memory.NewLocker(nil, nil, nil)
	must.NoError(t, err)

	scoped, err := distributedlock.NewScopedLocker(
		raw,
		nil, nil, nil,
		append([]distributedlock.ScopedOption{
			distributedlock.WithScopedLockTTL(scopedLockTTL),
			distributedlock.WithScopedPollInterval(scopedPollInterval),
			distributedlock.WithScopedJitter(noJitter),
		}, opts...)...,
	)
	must.NoError(t, err)

	return scoped, raw
}

// startWaiter parks a WithLock call on an already-held key and returns the
// channel its result lands on, once the adapter is asleep between polls.
func startWaiter(t *testing.T, ctx context.Context, scoped distributedlock.ScopedLocker) <-chan error {
	t.Helper()

	done := make(chan error, 1)
	go func() {
		done <- scoped.WithLock(ctx, scopedTestKey, func(context.Context) error {
			return nil
		})
	}()
	synctest.Wait()

	return done
}

// requireWaiting asserts the waiter has not acquired yet. Both this and
// requireAcquired use non-blocking receives: a blocking one would let the
// bubble idle forward to any later deadline and pass regardless of schedule.
func requireWaiting(t *testing.T, done <-chan error, when string) {
	t.Helper()

	synctest.Wait()
	select {
	case <-done:
		t.Fatalf("WithLock acquired %s, before its next poll came due", when)
	default:
	}
}

func requireAcquired(t *testing.T, done <-chan error, when string) {
	t.Helper()

	synctest.Wait()
	select {
	case err := <-done:
		must.NoError(t, err)
	default:
		t.Fatalf("WithLock had not acquired %s", when)
	}
}

func TestNewScopedLocker(T *testing.T) {
	T.Parallel()

	T.Run("rejects a nil locker", func(t *testing.T) {
		t.Parallel()

		_, err := distributedlock.NewScopedLocker(nil, nil, nil, nil)
		test.Error(t, err)
	})

	T.Run("rejects poll settings that would spin or shrink", func(t *testing.T) {
		t.Parallel()

		for _, tc := range []struct {
			opt  distributedlock.ScopedOption
			name string
		}{
			// A zero or negative interval is the dangerous one: clock.Sleep
			// returns immediately for it, so WithLock would busy-loop on a
			// contended lock rather than wait.
			{name: "zero poll interval", opt: distributedlock.WithScopedPollInterval(0)},
			{name: "negative poll interval", opt: distributedlock.WithScopedPollInterval(-time.Second)},
			{name: "shrinking backoff", opt: distributedlock.WithScopedPollBackoff(0.5, time.Second)},
			{name: "max below the poll interval", opt: distributedlock.WithScopedPollBackoff(2, time.Millisecond)},
		} {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				raw, err := memory.NewLocker(nil, nil, nil)
				must.NoError(t, err)

				_, err = distributedlock.NewScopedLocker(raw, nil, nil, nil,
					distributedlock.WithScopedPollInterval(scopedPollInterval), tc.opt)
				test.Error(t, err)
			})
		}
	})

	T.Run("accepts a backoff factor of exactly one", func(t *testing.T) {
		t.Parallel()

		raw, err := memory.NewLocker(nil, nil, nil)
		must.NoError(t, err)

		_, err = distributedlock.NewScopedLocker(raw, nil, nil, nil,
			distributedlock.WithScopedPollBackoff(1, distributedlock.DefaultScopedMaxPollInterval))
		test.NoError(t, err)
	})
}

func TestScopedLocker_WithLock_Backoff(T *testing.T) {
	T.Parallel()

	// Each case holds the lock past the first poll so a second wait is
	// scheduled, releases it, then steps the bubble to either side of the
	// second wait's deadline. Acquiring early or late fails, which pins the
	// exact interval rather than merely asserting "it eventually acquired".
	T.Run("the second wait is the first grown by the backoff factor", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			scoped, raw := newScopedFixture(t,
				distributedlock.WithScopedPollBackoff(2, time.Minute))

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			held, err := raw.Acquire(ctx, scopedTestKey, scopedLockTTL)
			must.NoError(t, err)

			done := startWaiter(t, ctx, scoped)

			// Burn the first wait against a still-held lock, scheduling a
			// second one of 2 x scopedPollInterval.
			time.Sleep(scopedPollInterval)
			requireWaiting(t, done, "on a held lock")

			must.NoError(t, held.Release(ctx))

			// One interval in, the un-grown schedule would already have fired.
			time.Sleep(scopedPollInterval)
			requireWaiting(t, done, "one poll interval after release")

			time.Sleep(scopedPollInterval)
			requireAcquired(t, done, "two poll intervals after release")
		})
	})

	T.Run("growth is clamped to the max poll interval", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			// A factor of 10 would make the second wait 1s; the max pins it to
			// 2 x scopedPollInterval instead.
			scoped, raw := newScopedFixture(t,
				distributedlock.WithScopedPollBackoff(10, 2*scopedPollInterval))

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			held, err := raw.Acquire(ctx, scopedTestKey, scopedLockTTL)
			must.NoError(t, err)

			done := startWaiter(t, ctx, scoped)

			time.Sleep(scopedPollInterval)
			requireWaiting(t, done, "on a held lock")

			must.NoError(t, held.Release(ctx))

			time.Sleep(scopedPollInterval)
			requireWaiting(t, done, "one poll interval after release")

			// Unclamped this would still be waiting at 2 intervals, with 8 to go.
			time.Sleep(scopedPollInterval)
			requireAcquired(t, done, "at the clamped max poll interval")
		})
	})

	T.Run("a factor of one keeps the interval fixed", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			scoped, raw := newScopedFixture(t,
				distributedlock.WithScopedPollBackoff(1, time.Minute))

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			held, err := raw.Acquire(ctx, scopedTestKey, scopedLockTTL)
			must.NoError(t, err)

			done := startWaiter(t, ctx, scoped)

			time.Sleep(scopedPollInterval)
			requireWaiting(t, done, "on a held lock")

			must.NoError(t, held.Release(ctx))

			// Ungrown, so the very next interval is the one that acquires.
			time.Sleep(scopedPollInterval)
			requireAcquired(t, done, "one poll interval after release")
		})
	})

	T.Run("jitter keeps each wait within half the interval", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			// The floor of the jitter range: every wait is exactly half the
			// interval, so the lock is taken half an interval after release.
			scoped, raw := newScopedFixture(t,
				distributedlock.WithScopedJitter(func() float64 { return 0 }),
				distributedlock.WithScopedPollBackoff(1, time.Minute))

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			held, err := raw.Acquire(ctx, scopedTestKey, scopedLockTTL)
			must.NoError(t, err)

			done := startWaiter(t, ctx, scoped)
			must.NoError(t, held.Release(ctx))

			time.Sleep(scopedPollInterval / 2)
			requireAcquired(t, done, "at half the poll interval with jitter at its floor")
		})
	})
}

func TestScopedLocker_TryWithLock(T *testing.T) {
	T.Parallel()

	T.Run("runs fn under the lock and releases after", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		scoped, raw := newScopedFixture(t)

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
		scoped, raw := newScopedFixture(t)

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
		scoped, raw := newScopedFixture(t)
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
		scoped, raw := newScopedFixture(t)

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
		scoped, _ := newScopedFixture(t)

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

		synctest.Test(t, func(t *testing.T) {
			scoped, raw := newScopedFixture(t)

			ctx, cancel := context.WithCancel(t.Context())
			// Unwind the waiter on the way out so a failed assertion reports
			// itself rather than stranding the bubble in a deadlock.
			defer cancel()

			held, err := raw.Acquire(ctx, scopedTestKey, scopedLockTTL)
			must.NoError(t, err)

			done := make(chan error, 1)
			go func() {
				done <- scoped.WithLock(ctx, scopedTestKey, func(context.Context) error {
					return nil
				})
			}()

			// Wait returns once the adapter is asleep between polls, so the
			// release below is what the next poll observes.
			synctest.Wait()
			must.NoError(t, held.Release(ctx))

			// Releasing alone must not wake it. Wait parks everything without
			// moving the clock, so a result here would mean the adapter acquired
			// outside its poll schedule.
			synctest.Wait()
			select {
			case <-done:
				t.Fatal("WithLock returned before its next poll came due")
			default:
			}

			// Crossing the poll deadline is what lets it through. Both checks are
			// non-blocking: a blocking receive would let the bubble idle forward
			// to any later deadline and pass regardless of the interval.
			time.Sleep(scopedPollInterval)
			synctest.Wait()
			select {
			case err = <-done:
				must.NoError(t, err)
			default:
				t.Fatal("WithLock did not acquire on the first poll after the release")
			}
		})
	})

	T.Run("a canceled context ends the wait", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			scoped, raw := newScopedFixture(t)

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

			// Cancel only once the adapter is parked in its poll sleep, so the
			// wake has to come from the context rather than from a poll tick.
			synctest.Wait()
			cancel()

			// Non-blocking, for the same reason as above: a blocking receive
			// would idle the bubble forward to the next poll and pass even if
			// cancellation were ignored.
			synctest.Wait()
			select {
			case err = <-done:
				test.ErrorIs(t, err, context.Canceled)
			default:
				t.Fatal("WithLock did not observe cancellation")
			}
		})
	})
}
