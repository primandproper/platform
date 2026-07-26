package clock

import (
	"context"
	"time"
)

// Clock is an injectable source of time. Production and test code alike
// receive the wall clock from NewClock: inside a testing/synctest bubble it
// reads the bubble's fake time, so no test double is needed. The interface
// deliberately covers only reading time, sleeping against it, and ticking on
// it — anything more (timers with resets, cron scheduling) belongs to the
// caller.
type Clock interface {
	// Now returns the current time.
	Now() time.Time

	// Since returns the time elapsed since t, per this clock's Now.
	Since(t time.Time) time.Duration

	// Sleep pauses the calling goroutine for d or until ctx is done,
	// whichever comes first, returning ctx.Err in the latter case. A
	// non-positive d does not sleep but still reports a done context, so a
	// pacing loop's cancellation check cannot be skipped by a zero delay.
	Sleep(ctx context.Context, d time.Duration) error

	// NewTicker returns a Ticker that delivers ticks every d. Like
	// time.NewTicker it panics if d is not positive, and slow receivers see
	// coalesced (dropped) ticks rather than a backlog. Callers must Stop the
	// ticker to release its resources.
	NewTicker(d time.Duration) Ticker
}

// Ticker delivers periodic ticks on a channel. It is the injectable
// counterpart of *time.Ticker, narrowed to the two members loops actually
// use.
type Ticker interface {
	// Chan returns the channel on which ticks are delivered.
	Chan() <-chan time.Time

	// Stop turns off the ticker. As with *time.Ticker, Stop does not close
	// the channel.
	Stop()
}
