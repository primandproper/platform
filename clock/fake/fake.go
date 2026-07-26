/*
Package clockfake provides a manually-advanced clock.Clock for tests. Time
moves only when the test calls Advance or Set, so TTL expiry, backoff pacing,
and periodic sweeps can be exercised deterministically without real sleeps.

A goroutine calling Sleep registers itself before blocking, and NewTicker
registers the ticker; BlockUntil lets the test wait for those registrations,
closing the race between starting the goroutine under test and advancing the
clock past its deadline.

Prefer testing/synctest where it fits: inside a synctest bubble the wall
clock from clock.NewClock already runs on the bubble's fake time, with
auto-advancing and built-in goroutine coordination (synctest.Wait). This
package is for the tests bubbles can't host — those doing real I/O — and
for tests that want time to move only at explicit Advance/Set calls.
*/
package clockfake

import (
	"context"
	"sync"
	"time"

	"github.com/primandproper/platform-go/v7/clock"
)

var _ clock.Clock = (*Clock)(nil)

// Clock is a clock.Clock whose time moves only via Advance or Set. The zero
// value is not usable; construct with New.
type Clock struct {
	now      time.Time
	sleepers map[*sleeper]struct{}
	tickers  map[*ticker]struct{}
	blockers []*blocker
	mu       sync.Mutex
}

// sleeper is one goroutine blocked in Sleep, released by closing done.
type sleeper struct {
	deadline time.Time
	done     chan struct{}
}

// blocker is one goroutine blocked in BlockUntil, released by closing ready.
type blocker struct {
	ready chan struct{}
	n     int
}

// New returns a Clock reading start until it is advanced.
func New(start time.Time) *Clock {
	return &Clock{
		now:      start,
		sleepers: map[*sleeper]struct{}{},
		tickers:  map[*ticker]struct{}{},
	}
}

// Now returns the clock's current time.
func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.now
}

// Since returns the time elapsed since t, per this clock's Now.
func (c *Clock) Since(t time.Time) time.Duration {
	return c.Now().Sub(t)
}

// Sleep blocks until the clock is advanced to or past now+d, or until ctx is
// done, whichever comes first, returning ctx.Err in the latter case. A
// non-positive d does not block but still reports a done context, matching
// the wall clock's behavior.
func (c *Clock) Sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}

	c.mu.Lock()
	s := &sleeper{deadline: c.now.Add(d), done: make(chan struct{})}
	c.sleepers[s] = struct{}{}
	c.notifyBlockersLocked()
	c.mu.Unlock()

	select {
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.sleepers, s)
		c.mu.Unlock()

		return ctx.Err()
	case <-s.done:
		return nil
	}
}

// NewTicker returns a Ticker that delivers a tick each time Advance or Set
// moves the clock across a multiple of d. Advancing across several intervals
// at once coalesces the missed ticks into one, mirroring how a slow receiver
// of a *time.Ticker sees dropped ticks. Like time.NewTicker it panics if d
// is not positive.
func (c *Clock) NewTicker(d time.Duration) clock.Ticker {
	if d <= 0 {
		panic("non-positive interval for NewTicker")
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	t := &ticker{
		clock:    c,
		ch:       make(chan time.Time, 1),
		interval: d,
		next:     c.now.Add(d),
	}
	c.tickers[t] = struct{}{}
	c.notifyBlockersLocked()

	return t
}

// Advance moves the clock forward by d, waking sleepers whose deadlines are
// reached and delivering any ticks that come due.
func (c *Clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.setLocked(c.now.Add(d))
}

// Set moves the clock to t, waking sleepers whose deadlines are reached and
// delivering any ticks that come due. Moving time backward is allowed but
// wakes nothing; like a wall clock, sleepers and tickers only fire moving
// forward.
func (c *Clock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.setLocked(t)
}

// Sleepers returns the number of goroutines currently blocked in Sleep,
// letting a test assert that a deadline was (or was not yet) reached.
func (c *Clock) Sleepers() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	return len(c.sleepers)
}

// BlockUntil blocks until at least n waiters — goroutines blocked in Sleep,
// plus live (unstopped) tickers — are registered with the clock. It closes
// the inherent race between starting a goroutine that will sleep or tick and
// advancing the clock past its deadline: advance only after BlockUntil
// returns, so the waiter is known to be registered.
func (c *Clock) BlockUntil(n int) {
	c.mu.Lock()

	if c.waiterCountLocked() >= n {
		c.mu.Unlock()
		return
	}

	b := &blocker{n: n, ready: make(chan struct{})}
	c.blockers = append(c.blockers, b)
	c.mu.Unlock()

	<-b.ready
}

// waiterCountLocked counts registered waiters: sleeping goroutines and live
// tickers. Callers must hold c.mu.
func (c *Clock) waiterCountLocked() int {
	return len(c.sleepers) + len(c.tickers)
}

// setLocked moves the clock to t and fires everything due. Callers must hold
// c.mu.
func (c *Clock) setLocked(t time.Time) {
	c.now = t

	for s := range c.sleepers {
		if !s.deadline.After(t) {
			close(s.done)
			delete(c.sleepers, s)
		}
	}

	for tk := range c.tickers {
		for !tk.next.After(t) {
			// A full buffer means the receiver hasn't consumed the previous
			// tick yet; drop this one, as *time.Ticker would.
			select {
			case tk.ch <- tk.next:
			default:
			}

			tk.next = tk.next.Add(tk.interval)
		}
	}
}

// notifyBlockersLocked releases BlockUntil callers whose threshold is now
// met. Callers must hold c.mu.
func (c *Clock) notifyBlockersLocked() {
	remaining := c.blockers[:0]

	for _, b := range c.blockers {
		if c.waiterCountLocked() >= b.n {
			close(b.ready)
		} else {
			remaining = append(remaining, b)
		}
	}

	c.blockers = remaining
}

// ticker implements clock.Ticker against the fake clock.
type ticker struct {
	clock    *Clock
	ch       chan time.Time
	next     time.Time
	interval time.Duration
}

// Chan returns the channel on which ticks are delivered.
func (t *ticker) Chan() <-chan time.Time {
	return t.ch
}

// Stop turns off the ticker. As with *time.Ticker, Stop does not close the
// channel. Stopping an already-stopped ticker is a no-op.
func (t *ticker) Stop() {
	t.clock.mu.Lock()
	defer t.clock.mu.Unlock()

	delete(t.clock.tickers, t)
}
