package clock

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// These tests pin the package's contract with testing/synctest: inside a
// bubble the wall Clock rides the bubble's fake time, because it delegates
// to the time package rather than caching anything at construction. Code
// that takes a clock.Clock can therefore be tested under synctest with the
// production implementation — no fake required.

func TestWallClock_InSynctestBubble(T *testing.T) {
	T.Parallel()

	T.Run("Sleep completes instantly on bubble time", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			c := NewClock()
			start := c.Now()

			// A durably-blocked sleeper makes the bubble's clock jump
			// straight to the deadline; this returns without real waiting.
			must.NoError(t, c.Sleep(t.Context(), time.Hour))

			test.EqOp(t, time.Hour, c.Since(start))
		})
	})

	T.Run("Ticker ticks on bubble time", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			c := NewClock()
			ticker := c.NewTicker(time.Minute)
			defer ticker.Stop()

			start := c.Now()
			tick := <-ticker.Chan()

			test.EqOp(t, start.Add(time.Minute), tick)
		})
	})

	T.Run("Sleep still honors cancellation inside a bubble", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			c := NewClock()
			ctx, cancel := context.WithCancel(t.Context())

			done := make(chan error, 1)
			go func() {
				done <- c.Sleep(ctx, time.Hour)
			}()

			// Once the sleeper is durably blocked on its timer, cancel: the
			// wake must come from the context, not from bubble time reaching
			// the deadline.
			synctest.Wait()
			cancel()

			test.ErrorIs(t, <-done, context.Canceled)
		})
	})
}
