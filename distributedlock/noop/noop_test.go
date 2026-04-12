package noop

import (
	"testing"
	"time"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

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
