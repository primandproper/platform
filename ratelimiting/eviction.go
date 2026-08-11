package ratelimiting

import (
	"cmp"
	"context"
	"math"
	"slices"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"
)

// DefaultMaxLimiters bounds how many per-key limiters the in-memory limiter
// holds at once unless WithMaxLimiters says otherwise.
//
// It is a memory ceiling, not a tuning parameter: at a few dozen bytes per
// entry this is single-digit megabytes, which is far above any legitimate count
// of distinct keys active within one window and far below what it costs to let
// a public endpoint's key space grow unchecked.
const DefaultMaxLimiters = 100_000

// minSweepInterval floors how often the sweep runs, whatever the window works
// out to.
//
// A window is as short as the configured rate makes it — thousands per second
// with a burst of one puts it under a millisecond — and a ticker at that cadence
// would spend more of the process scanning the map than serving from it.
// Sweeping less often than the TTL only delays reclamation, which costs memory
// rather than correctness: the TTL says when a limiter stopped being worth
// keeping, not when it must be gone.
const minSweepInterval = 100 * time.Millisecond

// capacityHeadroomDivisor is the fraction of the bound a capacity eviction
// frees beyond the overflow itself.
//
// Freeing exactly one slot would mean a full scan on every insert once the map
// sits at the bound. Taking it a sixteenth below buys that many inserts before
// the next pass, which is what keeps the cost of the bound proportional to the
// keys arriving rather than to the requests.
const capacityHeadroomDivisor = 16

// limiterEntry is one key's bucket, plus when it was last consulted.
type limiterEntry struct {
	limiter *rate.Limiter
	// lastSeen is a Unix nanosecond stamp rather than a time.Time so touching
	// it is a single atomic store. Allow touches it on every call, and a lock
	// here would serialize keys that otherwise share nothing.
	lastSeen atomic.Int64
}

func newLimiterEntry(limiter *rate.Limiter, now time.Time) *limiterEntry {
	entry := &limiterEntry{limiter: limiter}
	entry.touch(now)

	return entry
}

// touch records that the key was consulted at now.
func (e *limiterEntry) touch(now time.Time) {
	e.lastSeen.Store(now.UnixNano())
}

// idleFor reports how long the key has gone unconsulted as of now.
func (e *limiterEntry) idleFor(now time.Time) time.Duration {
	return now.Sub(time.Unix(0, e.lastSeen.Load()))
}

// limiterWindow is how long a key's bucket takes to refill from empty to a full
// burst at the steady rate, which is the longest a bucket can hold any history
// worth keeping.
//
// It is the same quantity ratelimiting/redis turns into the length of its
// sliding window, computed here in the token bucket's own units rather than in
// milliseconds. Both backends derive the lifetime of a key's state from it: the
// redis one expires its keys at twice the window, and this one evicts on the
// same principle.
//
// A zero or negative rate and a zero or negative burst are both read as one,
// matching the redis backend, so the window is always positive.
func limiterWindow(requestsPerSec float64, burstSize int) time.Duration {
	burst := max(float64(burstSize), 1)

	perSec := requestsPerSec
	if perSec <= 0 || math.IsNaN(perSec) {
		perSec = 1
	}

	if math.IsInf(perSec, 1) {
		return time.Nanosecond
	}

	return max(time.Duration(burst/perSec*float64(time.Second)), time.Nanosecond)
}

// sweepEvery evicts idle limiters on every tick until Close.
//
// Ticks come from the injected clock, so inside a testing/synctest bubble the
// sweep advances with the bubble's fake time and needs no test double.
//
// The context is this goroutine's own: it outlives every request that reaches
// the limiter, so there is no caller's context to inherit, and the only thing
// it carries is where the eviction counters record.
func (r *InMemoryRateLimiter) sweepEvery() {
	defer close(r.done)

	ctx := context.Background()

	ticker := r.clock.NewTicker(r.sweepInterval)
	defer ticker.Stop()

	for {
		select {
		case <-r.stop:
			return
		case <-ticker.Chan():
			r.sweep(ctx)
		}
	}
}

// sweep drops every limiter that has gone unconsulted for longer than the idle
// TTL.
//
// Dropping one changes no decision, which is what makes this safe to do behind
// the caller's back. The TTL is twice the window, and a bucket idle for a whole
// window has refilled to its full burst — so a key that comes back after the
// TTL is handed a limiter indistinguishable from the one it left behind. That
// is also why the TTL is derived rather than configurable: a shorter one would
// forgive debt a key still owes, and a longer one would only hold memory.
//
// Entries are deleted inside the scan, and compared on the way out: a key that
// was re-created between the read and the delete is a different entry, and
// CompareAndDelete leaves it alone. What that comparison cannot catch is a key
// touched — not re-created — in the same instant, which loses a bucket it had
// let go idle for a full TTL and is handed a full one. That is the same outcome
// as arriving a moment earlier.
func (r *InMemoryRateLimiter) sweep(ctx context.Context) {
	now := r.clock.Now()

	var evicted int64

	r.limiters.Range(func(key, value any) bool {
		k, isString := key.(string)

		entry, isEntry := value.(*limiterEntry)
		if !isString || !isEntry {
			return true
		}

		if entry.idleFor(now) < r.idleTTL {
			return true
		}

		if r.limiters.CompareAndDelete(k, entry) {
			r.tracked.Add(-1)
			evicted++
		}

		return true
	})

	if evicted > 0 {
		r.idleEvictedCounter.Add(ctx, evicted)
	}

	r.limitersGauge.Record(ctx, r.tracked.Load())
}

// evictOverflow brings the map back within the bound after an insert crossed
// it, dropping idle limiters first and then the least recently seen.
//
// The bound is the answer to the case the TTL cannot cover: a flood of distinct
// keys inside a single window, where nothing has been idle long enough to
// evict. Dropping a live limiter does forgive whatever its key still owed, and
// least-recently-seen is chosen because that is the bucket closest to being
// refilled — it is the cheapest one to forget, and it is the one an eviction
// changes least. The alternative is a map that grows until the process dies,
// which limits nothing at all.
//
// It runs on the goroutine that inserted the key, which is deliberate: the
// caller filling the map is the one that pays to bound it.
func (r *InMemoryRateLimiter) evictOverflow(ctx context.Context) {
	if !r.overBound() {
		return
	}

	// A pass already running will free the same slots, so a second one would
	// only scan the map again. The map may sit briefly over the bound while
	// that pass runs, by at most the number of concurrent inserts.
	if !r.evicting.TryLock() {
		return
	}
	defer r.evicting.Unlock()

	if !r.overBound() {
		return
	}

	// Idle limiters first: they are dead by the TTL's own rule, and evicting
	// them costs a key nothing. Only what is left over comes out of live ones.
	r.sweep(ctx)

	target := int64(max(r.maxLimiters-r.maxLimiters/capacityHeadroomDivisor, 1))

	over := r.tracked.Load() - target
	if over <= 0 {
		return
	}

	type aged struct {
		entry    *limiterEntry
		key      string
		lastSeen int64
	}

	candidates := make([]aged, 0, r.tracked.Load())

	r.limiters.Range(func(key, value any) bool {
		k, isString := key.(string)

		entry, isEntry := value.(*limiterEntry)
		if !isString || !isEntry {
			return true
		}

		candidates = append(candidates, aged{key: k, entry: entry, lastSeen: entry.lastSeen.Load()})

		return true
	})

	// Stamps are read once, during the scan, so a key touched while the pass
	// runs can still be evicted as the oldest. Re-reading would not fix it —
	// there is no instant at which the whole map is still — and the cost of
	// being wrong is one key's allowance, which the bound already trades away.
	slices.SortFunc(candidates, func(a, b aged) int { return cmp.Compare(a.lastSeen, b.lastSeen) })

	var evicted int64

	for i := range candidates {
		if evicted >= over {
			break
		}

		candidate := &candidates[i]
		if r.limiters.CompareAndDelete(candidate.key, candidate.entry) {
			r.tracked.Add(-1)
			evicted++
		}
	}

	if evicted > 0 {
		r.capacityEvictedCounter.Add(ctx, evicted)
	}

	r.limitersGauge.Record(ctx, r.tracked.Load())
}

// overBound reports whether the map holds more limiters than it may. A
// non-positive bound is no bound.
func (r *InMemoryRateLimiter) overBound() bool {
	return r.maxLimiters > 0 && r.tracked.Load() > int64(r.maxLimiters)
}
