package ratelimiting

import (
	"context"
	"sync"

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
