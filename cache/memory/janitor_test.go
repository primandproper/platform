package memory

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/primandproper/platform-go/v8/cache"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// size reports how many entries the map physically holds, expired or not.
//
// Every assertion about the janitor has to read the map directly: Get evicts
// what it discovers, so observing through the public surface would report the
// lazy path's work as the janitor's and the tests would pass with no janitor at
// all.
func size[T any](c *inMemoryCacheImpl[T]) int {
	c.cacheMu.RLock()
	defer c.cacheMu.RUnlock()

	return len(c.cache)
}

// newJanitorCache builds an expiry cache with a janitor already running,
// bound to a context the test cancels on the way out.
//
// The cancel is deliberately not deferred by the caller: the bubble does not
// close until every goroutine in it has exited, so a janitor left running would
// hang the test rather than leak silently. That makes "the goroutine stops when
// its context does" a property every one of these tests exercises, not just the
// one that names it.
func newJanitorCache(t *testing.T, defaultExpiry, interval time.Duration) *inMemoryCacheImpl[example] {
	t.Helper()

	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	c, err := NewInMemoryCache[example](defaultExpiry, nil, nil, nil, WithJanitor[example](ctx, interval))
	must.NoError(t, err)

	impl, ok := c.(*inMemoryCacheImpl[example])
	must.True(t, ok)

	return impl
}

func TestWithJanitor(T *testing.T) {
	T.Parallel()

	T.Run("sweeps expired entries with no read to discover them", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			ctx := t.Context()
			c := newJanitorCache(t, time.Minute, 10*time.Second)

			must.NoError(t, c.Set(ctx, exampleKey, &example{Name: t.Name()}))
			test.EqOp(t, 1, size(c))

			// Past the entry's deadline and across at least one tick.
			time.Sleep(time.Minute + 10*time.Second)

			test.EqOp(t, 0, size(c))
		})
	})

	T.Run("leaves unexpired entries alone", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			ctx := t.Context()
			c := newJanitorCache(t, time.Hour, time.Second)

			must.NoError(t, c.Set(ctx, exampleKey, &example{Name: t.Name()}))

			time.Sleep(time.Minute)

			test.EqOp(t, 1, size(c))
		})
	})

	T.Run("leaves NoExpiry entries alone", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			ctx := t.Context()
			c := newJanitorCache(t, time.Minute, time.Second)

			must.NoError(t, c.Set(ctx, exampleKey, &example{Name: t.Name()}, cache.WithExpiry(cache.NoExpiry)))

			time.Sleep(time.Hour)

			test.EqOp(t, 1, size(c))
		})
	})

	T.Run("sweeps only the entries that are actually expired", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			ctx := t.Context()
			c := newJanitorCache(t, time.Hour, time.Second)

			must.NoError(t, c.Set(ctx, "short", &example{Name: "short"}, cache.WithExpiry(time.Minute)))
			must.NoError(t, c.Set(ctx, "long", &example{Name: "long"}, cache.WithExpiry(time.Hour)))

			time.Sleep(2 * time.Minute)

			test.EqOp(t, 1, size(c))

			got, err := c.Get(ctx, "long")
			must.NoError(t, err)
			test.EqOp(t, "long", got.Name)
		})
	})

	T.Run("keeps sweeping across ticks", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			ctx := t.Context()
			c := newJanitorCache(t, 30*time.Second, time.Second)

			for range 3 {
				must.NoError(t, c.Set(ctx, exampleKey, &example{Name: t.Name()}))
				test.EqOp(t, 1, size(c))

				time.Sleep(time.Minute)
				test.EqOp(t, 0, size(c))
			}
		})
	})

	T.Run("counts one eviction per swept entry", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			ctx := t.Context()
			c := newJanitorCache(t, time.Minute, time.Second)
			counter := &countingCounter{}
			c.cacheEvictCounter = counter

			must.NoError(t, c.Set(ctx, "a", &example{Name: "a"}))
			must.NoError(t, c.Set(ctx, "b", &example{Name: "b"}))

			time.Sleep(2 * time.Minute)

			test.EqOp(t, int64(2), counter.Total())
		})
	})

	T.Run("stops when its context is cancelled", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())

			c, err := NewInMemoryCache[example](time.Minute, nil, nil, nil, WithJanitor[example](ctx, time.Second))
			must.NoError(t, err)

			impl, ok := c.(*inMemoryCacheImpl[example])
			must.True(t, ok)

			must.NoError(t, impl.Set(ctx, exampleKey, &example{Name: t.Name()}))
			cancel()

			// Let the janitor observe the cancellation and exit. Nothing is
			// swept afterwards, even well past the entry's deadline: the lazy
			// path is all that is left.
			synctest.Wait()
			time.Sleep(time.Hour)

			test.EqOp(t, 1, size(impl))
		})
	})

	T.Run("a non-positive interval starts no janitor", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			ctx := t.Context()

			for _, interval := range []time.Duration{0, -time.Second} {
				c, err := NewInMemoryCache[example](time.Minute, nil, nil, nil, WithJanitor[example](ctx, interval))
				must.NoError(t, err)

				impl, ok := c.(*inMemoryCacheImpl[example])
				must.True(t, ok)
				test.Nil(t, impl.janitor)

				must.NoError(t, impl.Set(ctx, exampleKey, &example{Name: t.Name()}))
				time.Sleep(time.Hour)

				test.EqOp(t, 1, size(impl))
			}
		})
	})

	T.Run("a nil context starts no janitor", func(t *testing.T) {
		t.Parallel()

		//nolint:staticcheck // passing a nil context is exactly what this guards.
		c, err := NewInMemoryCache[example](time.Minute, nil, nil, nil, WithJanitor[example](nil, time.Second))
		must.NoError(t, err)

		impl, ok := c.(*inMemoryCacheImpl[example])
		must.True(t, ok)
		test.Nil(t, impl.janitor)
	})

	T.Run("a nil option is ignored", func(t *testing.T) {
		t.Parallel()

		c, err := NewInMemoryCache[example](time.Minute, nil, nil, nil, nil)
		must.NoError(t, err)
		test.NotNil(t, c)
	})

	T.Run("no options behaves exactly as before", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			ctx := t.Context()
			c := newExpiryCache(t, time.Minute)

			must.NoError(t, c.Set(ctx, exampleKey, &example{Name: t.Name()}))
			time.Sleep(time.Hour)

			// Still resident: nothing swept it, and only a read will drop it.
			test.EqOp(t, 1, size(c))

			_, err := c.Get(ctx, exampleKey)
			test.ErrorIs(t, err, cache.ErrNotFound)
			test.EqOp(t, 0, size(c))
		})
	})
}

func TestInMemoryCache_sweep(T *testing.T) {
	T.Parallel()

	// The sweep collects candidates under the read lock and deletes them under
	// the write lock, so a value written in between must survive. Calling
	// evictExpired directly with a key that is no longer expired reproduces
	// that interleaving deterministically, which a timing-based test could not.
	T.Run("does not drop an entry overwritten after it was collected", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			ctx := t.Context()
			c, counter := newEvictionCountingCache(t, time.Minute)

			must.NoError(t, c.Set(ctx, exampleKey, &example{Name: "stale"}))
			time.Sleep(2 * time.Minute)

			// Stands in for the racing writer: the entry was expired when the
			// sweep listed it, and is fresh by the time the delete runs.
			must.NoError(t, c.Set(ctx, exampleKey, &example{Name: "fresh"}))
			c.evictExpired(ctx, exampleKey)

			got, err := c.Get(ctx, exampleKey)
			must.NoError(t, err)
			test.EqOp(t, "fresh", got.Name)
			test.EqOp(t, int64(0), counter.Total())
		})
	})

	T.Run("is a no-op on an empty cache", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			ctx := t.Context()
			c, counter := newEvictionCountingCache(t, time.Minute)

			c.sweep(ctx)

			test.EqOp(t, 0, size(c))
			test.EqOp(t, int64(0), counter.Total())
		})
	})
}
