package memory

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/shoenig/test/must"
)

type benchItem struct {
	Name string `json:"name"`
}

func BenchmarkInMemoryCache(b *testing.B) {
	c, err := NewInMemoryCache[benchItem](0)
	must.NoError(b, err)

	ctx := b.Context()
	val := &benchItem{Name: "value"}
	must.NoError(b, c.Set(ctx, "key", val))

	b.Run("Get", func(b *testing.B) {
		for b.Loop() {
			_, _ = c.Get(ctx, "key")
		}
	})

	b.Run("Set", func(b *testing.B) {
		for b.Loop() {
			_ = c.Set(ctx, "key", val)
		}
	})
}

// BenchmarkInMemoryCache_Janitor prices the janitor against the write path it
// contends with. The sweep takes the read lock to scan and the write lock to
// delete, so the cost it imposes is on concurrent writers, not on itself — a
// benchmark of the sweep alone would report a number nobody pays.
//
// The interval is deliberately short relative to the benchmark's runtime so
// sweeps actually land during the measured loop.
func BenchmarkInMemoryCache_Janitor(b *testing.B) {
	val := &benchItem{Name: "value"}

	run := func(b *testing.B, opts ...Option) {
		b.Helper()

		c, err := NewInMemoryCache[benchItem](time.Millisecond, opts...)
		must.NoError(b, err)

		ctx := b.Context()
		var i int
		for b.Loop() {
			i++
			_ = c.Set(ctx, strconv.Itoa(i%1024), val)
		}
	}

	b.Run("Off", func(b *testing.B) {
		run(b)
	})

	b.Run("On", func(b *testing.B) {
		run(b, WithJanitor(b.Context(), time.Millisecond))
	})
}

// BenchmarkInMemoryCache_Bound prices what each eviction policy costs the read
// path, which is the whole basis for offering two of them.
//
// The reads run in parallel because that is where the policies differ: an LRU
// bound has to record what it served, so its readers take the write lock and
// stop running concurrently with one another. A single-goroutine benchmark
// would price the bookkeeping and miss the contention, which is the larger
// number by far on a cache with readers to spare.
func BenchmarkInMemoryCache_Bound(b *testing.B) {
	const keys = 1024

	val := &benchItem{Name: "value"}

	run := func(b *testing.B, opts ...Option) {
		b.Helper()

		c, err := NewInMemoryCache[benchItem](0, opts...)
		must.NoError(b, err)

		ctx := b.Context()
		for i := range keys {
			must.NoError(b, c.Set(ctx, strconv.Itoa(i), val))
		}

		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			var i int
			for pb.Next() {
				i++
				_, _ = c.Get(ctx, strconv.Itoa(i%keys))
			}
		})
	}

	b.Run("Unbounded", func(b *testing.B) {
		run(b)
	})

	b.Run("OldestWritten", func(b *testing.B) {
		run(b, WithMaxEntries(keys, EvictOldestWritten))
	})

	b.Run("LeastRecentlyUsed", func(b *testing.B) {
		run(b, WithMaxEntries(keys, EvictLeastRecentlyUsed))
	})
}

// BenchmarkInMemoryCache_Loader prices a read-through miss against the flight
// that collapses it. The loader is deliberately trivial, so what is measured is
// the machinery rather than the work — a real loader's cost is the reason the
// flight exists, and would swamp everything else here.
func BenchmarkInMemoryCache_Loader(b *testing.B) {
	loader := func(_ context.Context, key string) (*benchItem, error) {
		return &benchItem{Name: key}, nil
	}

	b.Run("Hit", func(b *testing.B) {
		c, err := NewInMemoryCache[benchItem](0, WithLoader(loader))
		must.NoError(b, err)

		ctx := b.Context()
		_, err = c.Get(ctx, "key")
		must.NoError(b, err)

		for b.Loop() {
			_, _ = c.Get(ctx, "key")
		}
	})

	b.Run("Miss", func(b *testing.B) {
		c, err := NewInMemoryCache[benchItem](0, WithLoader(loader))
		must.NoError(b, err)

		ctx := b.Context()

		var i int
		for b.Loop() {
			i++
			key := strconv.Itoa(i)
			_, _ = c.Get(ctx, key)
			_ = c.Delete(ctx, key)
		}
	})
}
