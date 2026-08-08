package ratelimiting

import (
	"context"
	"math"
	"sync"
	"time"

	"github.com/primandproper/platform-go/v10/errors"
	"github.com/primandproper/platform-go/v10/observability"
	"github.com/primandproper/platform-go/v10/observability/metrics"

	"golang.org/x/time/rate"
)

const inMemoryName = "in_memory_rate_limiter"

// ErrRateLimited reports that a limiter refused an operation. Allow expresses a
// refusal as (false, nil) because the caller is usually deciding what to do
// next; this sentinel exists for the callers that have to hand the refusal back
// as an error instead — an http.RoundTripper, for one, has nowhere else to put
// it. Callers branch on it with errors.Is rather than on a bare false.
var ErrRateLimited = errors.New("rate limited")

// RateLimiter limits the rate of operations per key.
type RateLimiter interface {
	Allow(ctx context.Context, key string) (bool, error)
	Close() error
}

var (
	_ RateLimiter = (*inMemoryRateLimiter)(nil)
	_ RetryHinter = (*inMemoryRateLimiter)(nil)
)

type inMemoryRateLimiter struct {
	o11y            observability.Observer
	allowedCounter  metrics.Int64Counter
	rejectedCounter metrics.Int64Counter
	limiters        sync.Map
	requestsPerSec  float64
	burstSize       int
}

// NewInMemoryRateLimiter returns a RateLimiter that uses per-key limiters in memory.
func NewInMemoryRateLimiter(requestsPerSec float64, burstSize int, opts ...Option) (RateLimiter, error) {
	o := newOptions(opts)

	mp := metrics.EnsureMetricsProvider(o.metricsProvider)

	allowedCounter, err := mp.NewInt64Counter(inMemoryName + "_allowed")
	if err != nil {
		return nil, errors.Wrap(err, "creating allowed counter")
	}

	rejectedCounter, err := mp.NewInt64Counter(inMemoryName + "_rejected")
	if err != nil {
		return nil, errors.Wrap(err, "creating rejected counter")
	}

	return &inMemoryRateLimiter{
		o11y:            observability.NewObserver(inMemoryName, o.logger, o.tracerProvider),
		requestsPerSec:  requestsPerSec,
		burstSize:       burstSize,
		allowedCounter:  allowedCounter,
		rejectedCounter: rejectedCounter,
	}, nil
}

func (r *inMemoryRateLimiter) Allow(ctx context.Context, key string) (bool, error) {
	ctx, op := r.o11y.Begin(ctx)
	defer op.End()

	limiter := r.getOrCreateLimiter(ctx, key)
	allowed := limiter.Allow()
	if allowed {
		r.allowedCounter.Add(ctx, 1)
	} else {
		r.rejectedCounter.Add(ctx, 1)
	}
	return allowed, nil
}

// RetryAfter reports how long key's bucket needs to hold a whole token again.
//
// It reads the bucket rather than reserving from it. rate.Limiter.Reserve
// answers the same question, but spends the token to do it — so asking when to
// come back would itself push the answer further out, and a refused caller that
// asked twice would be told to wait longer for having asked.
//
// A key with no bucket yet reports no hint rather than zero: the caller is
// about to be allowed, so there is nothing to wait for and nothing to say.
func (r *inMemoryRateLimiter) RetryAfter(_ context.Context, key string) (time.Duration, bool) {
	value, ok := r.limiters.Load(key)
	if !ok {
		return 0, false
	}

	limiter, ok := value.(*rate.Limiter)
	if !ok {
		return 0, false
	}

	// A bucket that cannot hold a token never fills, so no wait would make the
	// next attempt succeed. Saying nothing is the honest answer.
	if limiter.Burst() < 1 {
		return 0, false
	}

	limit := float64(limiter.Limit())
	if limit <= 0 {
		return 0, false
	}

	deficit := 1 - limiter.Tokens()
	if deficit <= 0 || math.IsInf(limit, 1) {
		return 0, true
	}

	return time.Duration(deficit / limit * float64(time.Second)), true
}

func (r *inMemoryRateLimiter) getOrCreateLimiter(_ context.Context, key string) *rate.Limiter {
	if v, ok := r.limiters.Load(key); ok {
		if x, ok2 := v.(*rate.Limiter); ok2 {
			return x
		}
	}

	limiter := rate.NewLimiter(rate.Limit(r.requestsPerSec), r.burstSize)
	if v, loaded := r.limiters.LoadOrStore(key, limiter); loaded {
		if x, ok2 := v.(*rate.Limiter); ok2 {
			return x
		}
	}

	return limiter
}

func (r *inMemoryRateLimiter) Close() error {
	// Drop every per-key limiter so the map doesn't retain memory past shutdown.
	r.limiters.Clear()
	return nil
}
