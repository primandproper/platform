package ratelimiting

import (
	"strconv"
	"testing"
	"time"

	"github.com/shoenig/test/must"
)

// Allow runs on every request that passes through a rate-limited route, which
// makes it one of the few things in this module that a service pays for whether
// or not anything is wrong.
//
// The interesting axis is key cardinality, not throughput: the limiter holds
// one bucket per key in a sync.Map, so a per-user or per-IP limiter is doing a
// map lookup over a set that grows with traffic, while a per-route limiter is
// looking up one of a handful. The rows below move that axis deliberately.

func BenchmarkInMemoryRateLimiter_Allow(b *testing.B) {
	ctx := b.Context()

	// A generous limit, so the measured path is the allow path rather than the
	// rejection path — the two are benchmarked separately below.
	newLimiter := func(b *testing.B) RateLimiter {
		b.Helper()

		limiter, err := NewInMemoryRateLimiter(1e9, 1e9)
		must.NoError(b, err)
		b.Cleanup(func() { _ = limiter.Close() })

		return limiter
	}

	// One key is the per-route case: the same bucket every time, and the
	// cheapest the limiter gets.
	b.Run("singleKey", func(b *testing.B) {
		limiter := newLimiter(b)

		for b.Loop() {
			boolSink, _ = limiter.Allow(ctx, "route:/v1/charges")
		}
	})

	// A bounded key set is the per-tenant case.
	for _, keys := range []int{100, 10_000} {
		b.Run("keys="+strconv.Itoa(keys), func(b *testing.B) {
			limiter := newLimiter(b)

			// Populate first, so the loop measures steady-state lookups rather
			// than bucket construction.
			for i := range keys {
				_, _ = limiter.Allow(ctx, strconv.Itoa(i))
			}

			var i int
			for b.Loop() {
				i++
				boolSink, _ = limiter.Allow(ctx, strconv.Itoa(i%keys))
			}
		})
	}

	// Every key unseen, which is what a per-IP limiter facing a spray of
	// distinct sources actually does: a bucket is constructed and stored on
	// every call, and nothing ever evicts it.
	b.Run("everyKeyNew", func(b *testing.B) {
		limiter := newLimiter(b)

		var i int
		for b.Loop() {
			i++
			boolSink, _ = limiter.Allow(ctx, strconv.Itoa(i))
		}
	})

	// Contention is the real question for a shared limiter: the buckets live in
	// a sync.Map, but rate.Limiter takes its own mutex, so requests against one
	// key serialize on it.
	b.Run("parallel/singleKey", func(b *testing.B) {
		limiter := newLimiter(b)

		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				_, _ = limiter.Allow(ctx, "route:/v1/charges")
			}
		})
	})

	b.Run("parallel/manyKeys", func(b *testing.B) {
		limiter := newLimiter(b)

		const keys = 1024
		for i := range keys {
			_, _ = limiter.Allow(ctx, strconv.Itoa(i))
		}

		b.RunParallel(func(pb *testing.PB) {
			var i int
			for pb.Next() {
				i++
				_, _ = limiter.Allow(ctx, strconv.Itoa(i%keys))
			}
		})
	})
}

// BenchmarkInMemoryRateLimiter_Rejected is the path a limiter takes once it is
// actually limiting, which is when it is under the most load.
func BenchmarkInMemoryRateLimiter_Rejected(b *testing.B) {
	ctx := b.Context()

	// One token per second with no burst: the first call takes it and every
	// call in the loop is refused.
	limiter, err := NewInMemoryRateLimiter(1, 1)
	must.NoError(b, err)
	b.Cleanup(func() { _ = limiter.Close() })

	_, err = limiter.Allow(ctx, "key")
	must.NoError(b, err)

	b.Run("Allow", func(b *testing.B) {
		for b.Loop() {
			boolSink, _ = limiter.Allow(ctx, "key")
		}
	})

	// RetryAfterFor is what a refused caller is told, so it runs once per
	// rejection and its cost belongs next to the rejection's. It is measured
	// through the package function rather than the method because that is the
	// call every caller makes — the interface assertion it performs to find a
	// RetryHinter is part of what a retry hint costs.
	b.Run("RetryAfterFor", func(b *testing.B) {
		for b.Loop() {
			durationSink, boolSink = RetryAfterFor(ctx, limiter, "key")
		}
	})

	// An unknown key reports no hint and returns before touching the bucket.
	b.Run("RetryAfterFor/unknownKey", func(b *testing.B) {
		for b.Loop() {
			durationSink, boolSink = RetryAfterFor(ctx, limiter, "never-seen")
		}
	})
}

var (
	boolSink     bool
	durationSink time.Duration
)
