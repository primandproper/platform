package noop

import (
	"context"
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestRateLimiter_Allow(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		limiter := NewRateLimiter()
		ctx := context.Background()

		for range 100 {
			allowed, err := limiter.Allow(ctx, "any")
			must.NoError(t, err)
			test.True(t, allowed)
		}
	})
}

func TestRateLimiter_Close(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		limiter := NewRateLimiter()
		err := limiter.Close()
		must.NoError(t, err)
	})
}
