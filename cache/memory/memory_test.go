package memory

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/primandproper/platform-go/v7/cache"
	"github.com/primandproper/platform-go/v7/observability"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

const (
	exampleKey = "example"
)

type example struct {
	Name string `json:"name"`
}

// newRecordingCache builds an in-memory cache with a RecordingObserver swapped
// in, so a test can drive a method and assert that it opened and ended an
// operation.
func newRecordingCache(t *testing.T) (*inMemoryCacheImpl[example], *observability.RecordingObserver) {
	t.Helper()

	c, err := NewInMemoryCache[example](0, nil, nil, nil)
	must.NoError(t, err)

	obs := observability.NewRecordingObserver()
	impl := c.(*inMemoryCacheImpl[example])
	impl.o11y = obs

	return impl, obs
}

func Test_newInMemoryCache(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		actual, err := NewInMemoryCache[example](0, nil, nil, nil)
		must.NoError(t, err)
		test.NotNil(t, actual)
	})
}

func Test_inMemoryCacheImpl_Get(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		c, err := NewInMemoryCache[example](0, nil, nil, nil)
		must.NoError(t, err)

		expected := &example{Name: t.Name()}
		test.NoError(t, c.Set(ctx, exampleKey, expected))

		actual, err := c.Get(ctx, exampleKey)
		test.Eq(t, expected, actual)
		test.NoError(t, err)
	})

	T.Run("observes operation", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		c, obs := newRecordingCache(t)

		expected := &example{Name: t.Name()}
		test.NoError(t, c.Set(ctx, exampleKey, expected))

		actual, err := c.Get(ctx, exampleKey)
		test.Eq(t, expected, actual)
		test.NoError(t, err)

		// The cache methods attach no values, so assert the operation
		// lifecycle: Get opened and ended an operation with no errors.
		op := obs.ObservedOperationWithKeys(t)
		must.SliceEmpty(t, op.Errors)
	})
}

func Test_inMemoryCacheImpl_Set(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		c, err := NewInMemoryCache[example](0, nil, nil, nil)
		must.NoError(t, err)

		test.MapLen(t, 0, c.(*inMemoryCacheImpl[example]).cache)
		test.NoError(t, c.Set(ctx, exampleKey, &example{Name: t.Name()}))
		test.MapLen(t, 1, c.(*inMemoryCacheImpl[example]).cache)
	})
}

func Test_inMemoryCacheImpl_Delete(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		c, err := NewInMemoryCache[example](0, nil, nil, nil)
		must.NoError(t, err)

		test.MapLen(t, 0, c.(*inMemoryCacheImpl[example]).cache)
		test.NoError(t, c.Set(ctx, exampleKey, &example{Name: t.Name()}))
		test.MapLen(t, 1, c.(*inMemoryCacheImpl[example]).cache)
		test.NoError(t, c.Delete(ctx, exampleKey))
		test.MapLen(t, 0, c.(*inMemoryCacheImpl[example]).cache)
	})
}

func Test_inMemoryCacheImpl_GetMany(T *testing.T) {
	T.Parallel()

	T.Run("returns only hits", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		c, err := NewInMemoryCache[example](0, nil, nil, nil)
		must.NoError(t, err)

		hit := &example{Name: t.Name()}
		test.NoError(t, c.Set(ctx, "hit", hit))

		bc := c.(*inMemoryCacheImpl[example])
		out, getErr := bc.GetMany(ctx, []string{"hit", "miss"})
		test.NoError(t, getErr)
		test.MapLen(t, 1, out)
		test.Eq(t, hit, out["hit"])
	})

	T.Run("empty keys", func(t *testing.T) {
		t.Parallel()

		c, err := NewInMemoryCache[example](0, nil, nil, nil)
		must.NoError(t, err)

		out, getErr := c.(*inMemoryCacheImpl[example]).GetMany(t.Context(), nil)
		test.NoError(t, getErr)
		test.MapLen(t, 0, out)
	})
}

func Test_inMemoryCacheImpl_SetMany(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		c, err := NewInMemoryCache[example](0, nil, nil, nil)
		must.NoError(t, err)

		bc := c.(*inMemoryCacheImpl[example])
		test.MapLen(t, 0, bc.cache)

		test.NoError(t, bc.SetMany(ctx, map[string]*example{
			"a": {Name: "a"},
			"b": {Name: "b"},
		}))
		test.MapLen(t, 2, bc.cache)
	})
}

func Test_inMemoryCacheImpl_Ping(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		c, err := NewInMemoryCache[example](0, nil, nil, nil)
		must.NoError(t, err)
		test.NoError(t, c.Ping(t.Context()))
	})
}

// newExpiryCache builds a cache with the given default expiry. The expiry
// tests run inside a synctest bubble, where the production clock reads the
// bubble's fake time, so time.Sleep moves expiry forward without a real wait.
func newExpiryCache(t *testing.T, defaultExpiry time.Duration) *inMemoryCacheImpl[example] {
	t.Helper()

	c, err := NewInMemoryCache[example](defaultExpiry, nil, nil, nil)
	must.NoError(t, err)

	impl, ok := c.(*inMemoryCacheImpl[example])
	must.True(t, ok)

	return impl
}

func TestInMemoryCache_Expiry(T *testing.T) {
	T.Parallel()

	T.Run("entries expire after the default expiry", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			ctx := t.Context()
			c := newExpiryCache(t, time.Minute)

			must.NoError(t, c.Set(ctx, exampleKey, &example{Name: t.Name()}))

			// Bubble time lands on the deadline exactly, so the boundary itself
			// is testable: live a nanosecond before, gone a nanosecond later.
			time.Sleep(time.Minute - time.Nanosecond)
			_, err := c.Get(ctx, exampleKey)
			must.NoError(t, err)

			time.Sleep(time.Nanosecond)
			_, err = c.Get(ctx, exampleKey)
			test.ErrorIs(t, err, cache.ErrNotFound)
		})
	})

	T.Run("WithExpiry overrides the default per call", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			ctx := t.Context()
			c := newExpiryCache(t, time.Minute)

			must.NoError(t, c.Set(ctx, "short", &example{Name: "short"}, cache.WithExpiry(time.Second)))
			must.NoError(t, c.Set(ctx, "long", &example{Name: "long"}, cache.WithExpiry(time.Hour)))

			time.Sleep(time.Minute)

			_, err := c.Get(ctx, "short")
			test.ErrorIs(t, err, cache.ErrNotFound)

			_, err = c.Get(ctx, "long")
			test.NoError(t, err)
		})
	})

	T.Run("NoExpiry pins an entry against expiry", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			ctx := t.Context()
			c := newExpiryCache(t, time.Minute)

			must.NoError(t, c.Set(ctx, exampleKey, &example{Name: t.Name()}, cache.WithExpiry(cache.NoExpiry)))

			time.Sleep(1000 * time.Hour)

			_, err := c.Get(ctx, exampleKey)
			test.NoError(t, err)
		})
	})

	T.Run("non-positive default means entries never expire", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			ctx := t.Context()
			c := newExpiryCache(t, 0)

			must.NoError(t, c.Set(ctx, exampleKey, &example{Name: t.Name()}))

			time.Sleep(1000 * time.Hour)

			_, err := c.Get(ctx, exampleKey)
			test.NoError(t, err)
		})
	})

	T.Run("SetMany applies one expiry to the batch and GetMany filters expired entries", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			ctx := t.Context()
			c := newExpiryCache(t, time.Minute)

			must.NoError(t, c.SetMany(ctx, map[string]*example{
				"a": {Name: "a"},
				"b": {Name: "b"},
			}, cache.WithExpiry(time.Second)))
			must.NoError(t, c.Set(ctx, "c", &example{Name: "c"}))

			time.Sleep(time.Second)

			out, err := c.GetMany(ctx, []string{"a", "b", "c"})
			must.NoError(t, err)
			must.MapLen(t, 1, out)
			test.EqOp(t, "c", out["c"].Name)
		})
	})

	T.Run("expired entries are evicted lazily on read", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			ctx := t.Context()
			c := newExpiryCache(t, time.Second)

			must.NoError(t, c.Set(ctx, exampleKey, &example{Name: t.Name()}))
			time.Sleep(time.Second)

			_, err := c.Get(ctx, exampleKey)
			test.ErrorIs(t, err, cache.ErrNotFound)

			c.cacheMu.RLock()
			_, stillPresent := c.cache[exampleKey]
			c.cacheMu.RUnlock()
			test.False(t, stillPresent)
		})
	})

	T.Run("overwriting an expired entry revives the key", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			ctx := t.Context()
			c := newExpiryCache(t, time.Second)

			must.NoError(t, c.Set(ctx, exampleKey, &example{Name: "old"}))
			time.Sleep(time.Second)

			must.NoError(t, c.Set(ctx, exampleKey, &example{Name: "new"}))

			got, err := c.Get(ctx, exampleKey)
			must.NoError(t, err)
			test.EqOp(t, "new", got.Name)
		})
	})
}

func TestInMemoryCache_Deletion(T *testing.T) {
	T.Parallel()

	T.Run("DeleteMany removes only the named keys", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		c := newExpiryCache(t, 0)

		must.NoError(t, c.SetMany(ctx, map[string]*example{
			"a": {Name: "a"}, "b": {Name: "b"}, "c": {Name: "c"},
		}))

		must.NoError(t, c.DeleteMany(ctx, []string{"a", "b", "missing"}))

		out, err := c.GetMany(ctx, []string{"a", "b", "c"})
		must.NoError(t, err)
		must.MapLen(t, 1, out)
		test.EqOp(t, "c", out["c"].Name)
	})

	T.Run("DeleteByPrefix removes matching keys only", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		c := newExpiryCache(t, 0)

		must.NoError(t, c.SetMany(ctx, map[string]*example{
			"area:1:x": {Name: "1x"}, "area:1:y": {Name: "1y"}, "area:2:x": {Name: "2x"},
		}))

		must.NoError(t, c.DeleteByPrefix(ctx, "area:1:"))

		out, err := c.GetMany(ctx, []string{"area:1:x", "area:1:y", "area:2:x"})
		must.NoError(t, err)
		must.MapLen(t, 1, out)
		test.EqOp(t, "2x", out["area:2:x"].Name)
	})

	T.Run("Flush clears everything", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		c := newExpiryCache(t, 0)

		must.NoError(t, c.SetMany(ctx, map[string]*example{
			"a": {Name: "a"}, "b": {Name: "b"},
		}))

		must.NoError(t, c.Flush(ctx))

		out, err := c.GetMany(ctx, []string{"a", "b"})
		must.NoError(t, err)
		must.MapLen(t, 0, out)
	})
}
