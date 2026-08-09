package memory

import (
	"context"
	"time"
)

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
// A sweep bounds the map in time, not in size: it reclaims entries once they
// expire, and reclaims nothing before then. A keyspace that can produce entries
// faster than they expire — or one whose entries never expire at all — needs
// WithMaxEntries as well, which is the only thing here that puts a ceiling on
// the map.
//
// The sweep stops on Close, and also when ctx is done — whichever happens
// first. Passing context.Background() and relying on Close is the ordinary
// shape; a cancellable ctx is for tying the sweep to something narrower than
// the cache's own lifetime. A nil ctx or a non-positive interval starts no
// goroutine at all.
func WithJanitor(ctx context.Context, interval time.Duration) Option {
	return func(o *options) {
		if ctx == nil || interval <= 0 {
			return
		}

		o.janitorCtx = ctx
		o.janitorInterval = interval
	}
}

// sweepEvery sweeps on every tick until ctx is done.
//
// Ticks come from the injected clock, so inside a testing/synctest bubble the
// janitor advances with the bubble's fake time and needs no test double.
func (i *Cache[T]) sweepEvery(ctx context.Context, interval time.Duration) {
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
func (i *Cache[T]) sweep(ctx context.Context) {
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
