package ratelimiting

import (
	"context"
	"testing"
	"time"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// hintlessLimiter implements RateLimiter and nothing more.
type hintlessLimiter struct{}

func (hintlessLimiter) Allow(context.Context, string) (bool, error) { return false, nil }

func (hintlessLimiter) Close() error { return nil }

// hintingLimiter returns whatever it was told to.
type hintingLimiter struct {
	hintlessLimiter

	delay time.Duration
	ok    bool
}

func (h hintingLimiter) RetryAfter(context.Context, string) (time.Duration, bool) {
	return h.delay, h.ok
}

func TestRetryAfterFor(T *testing.T) {
	T.Parallel()

	T.Run("reports no hint for a limiter that is not a RetryHinter", func(t *testing.T) {
		t.Parallel()

		delay, ok := RetryAfterFor(t.Context(), hintlessLimiter{}, "key")
		test.False(t, ok)
		test.EqOp(t, time.Duration(0), delay)
	})

	T.Run("passes a hinter's answer through", func(t *testing.T) {
		t.Parallel()

		delay, ok := RetryAfterFor(t.Context(), hintingLimiter{delay: 250 * time.Millisecond, ok: true}, "key")
		must.True(t, ok)
		test.EqOp(t, 250*time.Millisecond, delay)
	})

	T.Run("reports no hint when the hinter declines", func(t *testing.T) {
		t.Parallel()

		delay, ok := RetryAfterFor(t.Context(), hintingLimiter{delay: time.Second, ok: false}, "key")
		test.False(t, ok)
		test.EqOp(t, time.Duration(0), delay)
	})

	T.Run("discards a negative hint rather than passing it on", func(t *testing.T) {
		t.Parallel()

		// A wait in the past is the same as no wait at all, and a caller that
		// rendered it would tell a client to come back before it was refused.
		delay, ok := RetryAfterFor(t.Context(), hintingLimiter{delay: -time.Second, ok: true}, "key")
		test.False(t, ok)
		test.EqOp(t, time.Duration(0), delay)
	})
}

func TestInMemoryRateLimiter_RetryAfter(T *testing.T) {
	T.Parallel()

	T.Run("reports no hint for a key it has never seen", func(t *testing.T) {
		t.Parallel()

		limiter, err := NewInMemoryRateLimiter(10, 1)
		must.NoError(t, err)
		defer limiter.Close()

		delay, ok := limiter.(RetryHinter).RetryAfter(t.Context(), "unseen")
		test.False(t, ok)
		test.EqOp(t, time.Duration(0), delay)
	})

	T.Run("estimates the refill wait after a refusal", func(t *testing.T) {
		t.Parallel()

		// One token per second, burst of one: spending the token leaves the
		// bucket a full second from holding another.
		limiter, err := NewInMemoryRateLimiter(1, 1)
		must.NoError(t, err)
		defer limiter.Close()

		allowed, err := limiter.Allow(t.Context(), "key")
		must.NoError(t, err)
		must.True(t, allowed)

		allowed, err = limiter.Allow(t.Context(), "key")
		must.NoError(t, err)
		must.False(t, allowed)

		delay, ok := limiter.(RetryHinter).RetryAfter(t.Context(), "key")
		must.True(t, ok)
		test.Greater(t, time.Duration(0), delay)
		test.LessEq(t, time.Second, delay)
	})

	T.Run("asking twice does not push the answer out", func(t *testing.T) {
		t.Parallel()

		// The regression this guards: rate.Limiter.Reserve would answer the
		// same question by spending a token, so a refused caller that asked
		// twice would be told to wait longer for having asked.
		limiter, err := NewInMemoryRateLimiter(1, 1)
		must.NoError(t, err)
		defer limiter.Close()

		allowed, err := limiter.Allow(t.Context(), "key")
		must.NoError(t, err)
		must.True(t, allowed)

		first, ok := limiter.(RetryHinter).RetryAfter(t.Context(), "key")
		must.True(t, ok)

		second, ok := limiter.(RetryHinter).RetryAfter(t.Context(), "key")
		must.True(t, ok)

		// Time passes between the two calls, so the second can only be shorter.
		test.LessEq(t, first, second)
	})

	T.Run("reports zero for a key with a token in hand", func(t *testing.T) {
		t.Parallel()

		limiter, err := NewInMemoryRateLimiter(10, 5)
		must.NoError(t, err)
		defer limiter.Close()

		allowed, err := limiter.Allow(t.Context(), "key")
		must.NoError(t, err)
		must.True(t, allowed)

		delay, ok := limiter.(RetryHinter).RetryAfter(t.Context(), "key")
		must.True(t, ok)
		test.EqOp(t, time.Duration(0), delay)
	})

	T.Run("reports no hint for a bucket that can never hold a token", func(t *testing.T) {
		t.Parallel()

		// Burst zero refuses everything forever, so no wait would help and
		// naming one would be a lie.
		limiter, err := NewInMemoryRateLimiter(10, 0)
		must.NoError(t, err)
		defer limiter.Close()

		allowed, err := limiter.Allow(t.Context(), "key")
		must.NoError(t, err)
		must.False(t, allowed)

		delay, ok := limiter.(RetryHinter).RetryAfter(t.Context(), "key")
		test.False(t, ok)
		test.EqOp(t, time.Duration(0), delay)
	})

	T.Run("reports no hint for a bucket that never refills", func(t *testing.T) {
		t.Parallel()

		// A zero rate hands out the burst once and then never refills, so the
		// refusal after it is permanent and there is no wait to name.
		limiter, err := NewInMemoryRateLimiter(0, 1)
		must.NoError(t, err)
		defer limiter.Close()

		allowed, err := limiter.Allow(t.Context(), "key")
		must.NoError(t, err)
		must.True(t, allowed)

		allowed, err = limiter.Allow(t.Context(), "key")
		must.NoError(t, err)
		must.False(t, allowed)

		_, ok := limiter.(RetryHinter).RetryAfter(t.Context(), "key")
		test.False(t, ok)
	})
}
