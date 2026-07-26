package clock

import (
	"context"
	"time"
)

// wallClock is the production Clock, delegating to the time package.
type wallClock struct{}

// NewClock returns the wall Clock backed by the time package.
func NewClock() Clock {
	return wallClock{}
}

// Now returns the current wall-clock time.
func (wallClock) Now() time.Time {
	return time.Now()
}

// Since returns the time elapsed since t.
func (wallClock) Since(t time.Time) time.Duration {
	return time.Since(t)
}

// Sleep pauses for d or until ctx is done, whichever comes first.
func (wallClock) Sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}

	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// NewTicker returns a Ticker backed by a *time.Ticker.
func (wallClock) NewTicker(d time.Duration) Ticker {
	return wallTicker{ticker: time.NewTicker(d)}
}

// wallTicker adapts *time.Ticker to the Ticker interface.
type wallTicker struct {
	ticker *time.Ticker
}

// Chan returns the underlying ticker's channel.
func (t wallTicker) Chan() <-chan time.Time {
	return t.ticker.C
}

// Stop turns off the underlying ticker.
func (t wallTicker) Stop() {
	t.ticker.Stop()
}
