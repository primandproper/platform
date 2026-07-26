package clockfake

import (
	"context"
	"testing"
	"time"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

var testStart = time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

func TestClock_Now(T *testing.T) {
	T.Parallel()

	T.Run("reads the start time until advanced", func(t *testing.T) {
		t.Parallel()

		c := New(testStart)

		test.EqOp(t, testStart, c.Now())

		c.Advance(time.Minute)

		test.EqOp(t, testStart.Add(time.Minute), c.Now())
	})

	T.Run("Set moves to an absolute time", func(t *testing.T) {
		t.Parallel()

		c := New(testStart)
		target := testStart.Add(48 * time.Hour)

		c.Set(target)

		test.EqOp(t, target, c.Now())
	})
}

func TestClock_Since(T *testing.T) {
	T.Parallel()

	T.Run("measures advanced time", func(t *testing.T) {
		t.Parallel()

		c := New(testStart)
		c.Advance(90 * time.Second)

		test.EqOp(t, 90*time.Second, c.Since(testStart))
	})
}

func TestClock_Sleep(T *testing.T) {
	T.Parallel()

	T.Run("wakes when the clock reaches the deadline", func(t *testing.T) {
		t.Parallel()

		c := New(testStart)
		done := make(chan error, 1)

		go func() {
			done <- c.Sleep(context.Background(), 10*time.Second)
		}()

		c.BlockUntil(1)

		// Partway there: the sleeper must still be blocked.
		c.Advance(9 * time.Second)
		test.EqOp(t, 1, c.Sleepers())

		c.Advance(time.Second)

		select {
		case err := <-done:
			must.NoError(t, err)
		case <-time.After(time.Second):
			t.Fatal("sleeper was not woken at its deadline")
		}

		test.EqOp(t, 0, c.Sleepers())
	})

	T.Run("returns when the context is canceled", func(t *testing.T) {
		t.Parallel()

		c := New(testStart)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)

		go func() {
			done <- c.Sleep(ctx, time.Hour)
		}()

		c.BlockUntil(1)
		cancel()

		select {
		case err := <-done:
			test.ErrorIs(t, err, context.Canceled)
		case <-time.After(time.Second):
			t.Fatal("sleeper did not observe cancellation")
		}

		test.EqOp(t, 0, c.Sleepers())
	})

	T.Run("non-positive duration does not block but reports a done context", func(t *testing.T) {
		t.Parallel()

		c := New(testStart)

		must.NoError(t, c.Sleep(context.Background(), 0))

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		test.ErrorIs(t, c.Sleep(ctx, -time.Second), context.Canceled)
	})
}

func TestClock_BlockUntil(T *testing.T) {
	T.Parallel()

	T.Run("returns immediately when enough sleepers exist", func(t *testing.T) {
		t.Parallel()

		c := New(testStart)

		go func() {
			_ = c.Sleep(context.Background(), time.Minute)
		}()

		c.BlockUntil(1)
		// Already satisfied — must not block.
		c.BlockUntil(1)

		c.Advance(time.Minute)
	})
}

func TestClock_NewTicker(T *testing.T) {
	T.Parallel()

	T.Run("delivers a tick when an interval elapses", func(t *testing.T) {
		t.Parallel()

		c := New(testStart)
		ticker := c.NewTicker(time.Second)
		defer ticker.Stop()

		c.Advance(time.Second)

		select {
		case tick := <-ticker.Chan():
			test.EqOp(t, testStart.Add(time.Second), tick)
		default:
			t.Fatal("expected a tick after one interval")
		}
	})

	T.Run("coalesces ticks across a large advance", func(t *testing.T) {
		t.Parallel()

		c := New(testStart)
		ticker := c.NewTicker(time.Second)
		defer ticker.Stop()

		c.Advance(3 * time.Second)

		// Three intervals elapsed with nobody receiving: exactly one tick is
		// buffered, the rest were dropped, as with a *time.Ticker.
		select {
		case tick := <-ticker.Chan():
			test.EqOp(t, testStart.Add(time.Second), tick)
		default:
			t.Fatal("expected a buffered tick")
		}

		select {
		case tick := <-ticker.Chan():
			t.Fatalf("expected coalesced ticks, got a second tick at %v", tick)
		default:
		}

		// The schedule stays aligned: the next interval boundary is 4s.
		c.Advance(time.Second)

		select {
		case tick := <-ticker.Chan():
			test.EqOp(t, testStart.Add(4*time.Second), tick)
		default:
			t.Fatal("expected a tick at the next interval boundary")
		}
	})

	T.Run("stopped tickers deliver nothing", func(t *testing.T) {
		t.Parallel()

		c := New(testStart)
		ticker := c.NewTicker(time.Second)
		ticker.Stop()
		// A second Stop is a no-op, as with *time.Ticker.
		ticker.Stop()

		c.Advance(5 * time.Second)

		select {
		case tick := <-ticker.Chan():
			t.Fatalf("stopped ticker delivered a tick at %v", tick)
		default:
		}
	})

	T.Run("panics on a non-positive interval", func(t *testing.T) {
		t.Parallel()

		defer func() {
			if recover() == nil {
				t.Fatal("expected NewTicker(0) to panic")
			}
		}()

		New(testStart).NewTicker(0)
	})
}

func TestClock_BlockUntil_Tickers(T *testing.T) {
	T.Parallel()

	T.Run("a live ticker counts as a waiter", func(t *testing.T) {
		t.Parallel()

		c := New(testStart)
		ticker := c.NewTicker(time.Second)
		defer ticker.Stop()

		// Satisfied by the ticker alone — must not block.
		c.BlockUntil(1)
	})

	T.Run("waits for a ticker registered by another goroutine", func(t *testing.T) {
		t.Parallel()

		c := New(testStart)
		got := make(chan time.Time, 1)

		go func() {
			ticker := c.NewTicker(time.Second)
			defer ticker.Stop()
			got <- <-ticker.Chan()
		}()

		// Once BlockUntil returns the ticker exists, so this Advance
		// deterministically produces the first tick.
		c.BlockUntil(1)
		c.Advance(time.Second)

		select {
		case tick := <-got:
			test.EqOp(t, testStart.Add(time.Second), tick)
		case <-time.After(5 * time.Second):
			t.Fatal("ticker never fired after Advance")
		}
	})
}
