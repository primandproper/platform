package noop

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// This package runs no distributedlocktest suite, and the omission is the
// point rather than an oversight. Mutual exclusion is the whole of what those
// suites assert, and this provider arbitrates none of it by design: Acquire
// always succeeds, Release always reports success, and no TTL is ever
// enforced. Declaring enough deviations to get it through would leave a suite
// that checks the shape of the methods, which the compiler already does.
//
// What is worth pinning here is that the handles are consistent with what
// Acquire was asked for, which is all a caller can observe — and the tests
// below are that.

func TestNewLocker(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()
		test.NotNil(t, NewLocker())
	})
}

func TestLocker_Acquire(T *testing.T) {
	T.Parallel()

	T.Run("returns a usable handle", func(t *testing.T) {
		t.Parallel()
		l := NewLocker()
		lock, err := l.Acquire(t.Context(), "k", time.Second)
		must.NoError(t, err)
		must.NotNil(t, lock)
		test.EqOp(t, "k", lock.Key())
		test.EqOp(t, time.Second, lock.TTL())
	})

	T.Run("contended acquires both succeed", func(t *testing.T) {
		t.Parallel()
		l := NewLocker()
		_, err := l.Acquire(t.Context(), "shared", time.Second)
		must.NoError(t, err)
		_, err = l.Acquire(t.Context(), "shared", time.Second)
		must.NoError(t, err)
	})
}

func TestLocker_Ping(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()
		must.NoError(t, NewLocker().Ping(t.Context()))
	})
}

func TestLocker_Close(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()
		must.NoError(t, NewLocker().Close())
	})
}

func TestLock_ReleaseAndRefresh(T *testing.T) {
	T.Parallel()

	T.Run("release is a no-op", func(t *testing.T) {
		t.Parallel()
		l, err := NewLocker().Acquire(t.Context(), "k", time.Second)
		must.NoError(t, err)
		must.NoError(t, l.Release(t.Context()))
		must.NoError(t, l.Release(t.Context()))
	})

	T.Run("refresh updates ttl", func(t *testing.T) {
		t.Parallel()
		l, err := NewLocker().Acquire(t.Context(), "k", time.Second)
		must.NoError(t, err)
		must.NoError(t, l.Refresh(t.Context(), 5*time.Second))
		test.EqOp(t, 5*time.Second, l.TTL())
	})
}

func TestNewScopedLocker(T *testing.T) {
	T.Parallel()

	T.Run("WithLock runs fn as if the lock were free", func(t *testing.T) {
		t.Parallel()

		s := NewScopedLocker()
		must.NotNil(t, s)

		ran := false
		must.NoError(t, s.WithLock(t.Context(), "chore", func(context.Context) error {
			ran = true
			return nil
		}))
		test.True(t, ran)
	})

	T.Run("WithLock passes fn's error through", func(t *testing.T) {
		t.Parallel()

		boom := errors.New("boom")
		test.ErrorIs(t, NewScopedLocker().WithLock(t.Context(), "chore", func(context.Context) error {
			return boom
		}), boom)
	})

	T.Run("TryWithLock always reports acquired", func(t *testing.T) {
		t.Parallel()

		ran := false
		acquired, err := NewScopedLocker().TryWithLock(t.Context(), "chore", func(context.Context) error {
			ran = true
			return nil
		})
		must.NoError(t, err)
		test.True(t, acquired)
		test.True(t, ran)
	})

	T.Run("TryWithLock passes fn's error through", func(t *testing.T) {
		t.Parallel()

		boom := errors.New("boom")
		acquired, err := NewScopedLocker().TryWithLock(t.Context(), "chore", func(context.Context) error {
			return boom
		})
		test.ErrorIs(t, err, boom)
		// Acquisition succeeded; only fn failed.
		test.True(t, acquired)
	})
}
