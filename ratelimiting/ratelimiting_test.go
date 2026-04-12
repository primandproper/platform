package ratelimiting

import (
	"context"
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestInMemoryRateLimiter_Allow(T *testing.T) {
	T.Parallel()

	T.Run("allows within burst", func(t *testing.T) {
		t.Parallel()

		limiter, err := NewInMemoryRateLimiter(nil, 10, 3)
		must.NoError(t, err)
		defer limiter.Close()

		ctx := context.Background()

		allowed, err := limiter.Allow(ctx, "key1")
		must.NoError(t, err)
		test.True(t, allowed)

		allowed, err = limiter.Allow(ctx, "key1")
		must.NoError(t, err)
		test.True(t, allowed)

		allowed, err = limiter.Allow(ctx, "key1")
		must.NoError(t, err)
		test.True(t, allowed)

		allowed, err = limiter.Allow(ctx, "key1")
		must.NoError(t, err)
		test.False(t, allowed)
	})

	T.Run("different keys have independent limits", func(t *testing.T) {
		t.Parallel()

		limiter, err := NewInMemoryRateLimiter(nil, 10, 1)
		must.NoError(t, err)
		defer limiter.Close()

		ctx := context.Background()

		allowed, err := limiter.Allow(ctx, "key1")
		must.NoError(t, err)
		test.True(t, allowed)

		allowed, err = limiter.Allow(ctx, "key2")
		must.NoError(t, err)
		test.True(t, allowed)

		allowed, err = limiter.Allow(ctx, "key1")
		must.NoError(t, err)
		test.False(t, allowed)

		allowed, err = limiter.Allow(ctx, "key2")
		must.NoError(t, err)
		test.False(t, allowed)
	})

	T.Run("Close is safe", func(t *testing.T) {
		t.Parallel()

		limiter, err := NewInMemoryRateLimiter(nil, 10, 1)
		must.NoError(t, err)
		err = limiter.Close()
		must.NoError(t, err)
	})
}
