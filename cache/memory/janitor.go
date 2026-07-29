package memory

import (
	"context"
	"time"
)

// Option configures an in-memory cache at construction. Options are applied in
// the order given, and a nil option is ignored.
type Option[T any] func(*inMemoryCacheImpl[T])

// WithJanitor starts a background sweep that removes expired entries every
// interval, rather than waiting for a read to discover them.
//
// Without it an entry is only dropped when something reads that exact key or
// overwrites it. That is fine for a hot cache, where the keys worth evicting
// are the keys being read anyway. It is not fine for a workload that writes
// many keys and rarely reads them back — an idempotency-key store, a long-TTL
// request cache — because nothing ever triggers the lazy path and the map grows
// without bound. The rule of thumb: enable it whenever the expiry is long
// relative to how often a given key is read.
//
// The sweep is bound to ctx rather than to a Close method because cache.Cache
// has no Close, and adding one would touch every provider and every mock. The
// goroutine exits when ctx is done, so a caller that needs the sweep to stop
// before process exit passes a cancellable context. A nil ctx or a non-positive
// interval starts no goroutine at all.
func WithJanitor[T any](ctx context.Context, interval time.Duration) Option[T] {
	return func(i *inMemoryCacheImpl[T]) {
		if ctx == nil || interval <= 0 {
			return
		}

		// The starter is deferred to the end of construction rather than run
		// here so the janitor never observes a half-built cache, and so the
		// context lives in this closure instead of on the struct.
		i.janitor = func() { go i.sweepEvery(ctx, interval) }
	}
}

// sweepEvery sweeps on every tick until ctx is done.
//
// Ticks come from the injected clock, so inside a testing/synctest bubble the
// janitor advances with the bubble's fake time and needs no test double.
func (i *inMemoryCacheImpl[T]) sweepEvery(ctx context.Context, interval time.Duration) {
	ticker := i.clock.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.Chan():
			i.sweep(ctx)
		}
	}
}

// sweep removes every entry whose deadline has passed.
//
// Candidates are collected under the read lock and deleted through
// evictExpired, which re-checks expiry under the write lock. The split is what
// keeps the write lock short: one Lock held across a full map scan would stall
// every reader for the length of the scan, and the scan grows with the very
// backlog the janitor exists to clear. It is also what makes the sweep safe —
// an entry a caller overwrites between the two phases is no longer expired when
// evictExpired looks again, so the fresh value survives.
//
// Eviction accounting is left to evictExpired, so a swept entry counts exactly
// like one discovered by a read: both are TTL losses, and the counter means the
// same thing whether or not a janitor is running.
func (i *inMemoryCacheImpl[T]) sweep(ctx context.Context) {
	var expired []string

	i.cacheMu.RLock()
	for key, e := range i.cache {
		if i.expired(e) {
			expired = append(expired, key)
		}
	}
	i.cacheMu.RUnlock()

	for _, key := range expired {
		i.evictExpired(ctx, key)
	}
}
