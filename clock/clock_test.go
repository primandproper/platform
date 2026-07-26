package clock

import (
	"context"
	"testing"
	"time"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestWallClock_Now(T *testing.T) {
	T.Parallel()

	T.Run("tracks the time package", func(t *testing.T) {
		t.Parallel()

		c := NewClock()
		before := time.Now()
		now := c.Now()
		after := time.Now()

		test.False(t, now.Before(before))
		test.False(t, now.After(after))
	})
}

func TestWallClock_Since(T *testing.T) {
	T.Parallel()

	T.Run("elapsed time is non-negative and grows", func(t *testing.T) {
		t.Parallel()

		c := NewClock()
		start := c.Now()

		test.True(t, c.Since(start) >= 0)
	})
}

func TestWallClock_Sleep(T *testing.T) {
	T.Parallel()

	T.Run("sleeps for the requested duration", func(t *testing.T) {
		t.Parallel()

		c := NewClock()
		d := 5 * time.Millisecond
		start := time.Now()

		err := c.Sleep(context.Background(), d)

		must.NoError(t, err)
		test.True(t, time.Since(start) >= d)
	})

	T.Run("returns promptly when the context is canceled", func(t *testing.T) {
		t.Parallel()

		c := NewClock()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := c.Sleep(ctx, time.Hour)

		test.ErrorIs(t, err, context.Canceled)
	})

	T.Run("non-positive duration does not sleep but reports a done context", func(t *testing.T) {
		t.Parallel()

		c := NewClock()

		must.NoError(t, c.Sleep(context.Background(), 0))

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		test.ErrorIs(t, c.Sleep(ctx, -time.Second), context.Canceled)
	})
}

func TestWallClock_NewTicker(T *testing.T) {
	T.Parallel()

	T.Run("delivers ticks", func(t *testing.T) {
		t.Parallel()

		c := NewClock()
		ticker := c.NewTicker(time.Millisecond)
		defer ticker.Stop()

		timeout := time.After(time.Second)

		for range 2 {
			select {
			case <-ticker.Chan():
			case <-timeout:
				t.Fatal("timed out waiting for a tick")
			}
		}
	})
}
