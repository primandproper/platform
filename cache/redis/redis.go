package redis

import (
	"bytes"
	"context"
	"encoding/gob"
	"fmt"
	"strings"
	"time"

	"github.com/primandproper/platform/cache"
	"github.com/primandproper/platform/circuitbreaking"
	circuitbreakingcfg "github.com/primandproper/platform/circuitbreaking/config"
	"github.com/primandproper/platform/errors"
	"github.com/primandproper/platform/observability/logging"
	"github.com/primandproper/platform/observability/metrics"
	"github.com/primandproper/platform/observability/tracing"

	"github.com/redis/go-redis/v9"
)

const name = "redis_cache"

type redisClient interface {
	Get(ctx context.Context, key string) *redis.StringCmd
	Set(ctx context.Context, key string, value any, expiration time.Duration) *redis.StatusCmd
	Del(ctx context.Context, keys ...string) *redis.IntCmd
	Ping(ctx context.Context) *redis.StatusCmd
}

type redisCacheImpl[T any] struct {
	logger           logging.Logger
	tracer           tracing.Tracer
	cacheHitCounter  metrics.Int64Counter
	cacheMissCounter metrics.Int64Counter
	cacheSetCounter  metrics.Int64Counter
	cacheDelCounter  metrics.Int64Counter
	cacheErrCounter  metrics.Int64Counter
	latencyHist      metrics.Float64Histogram
	client           redisClient
	circuitBreaker   circuitbreaking.CircuitBreaker
	expiration       time.Duration
}

// NewRedisCache builds a new redis-backed cache.
func NewRedisCache[T any](cfg *Config, expiration time.Duration, logger logging.Logger, tracerProvider tracing.TracerProvider, metricsProvider metrics.Provider, cb circuitbreaking.CircuitBreaker) (cache.Cache[T], error) {
	mp := metrics.EnsureMetricsProvider(metricsProvider)

	cacheHitCounter, err := mp.NewInt64Counter(fmt.Sprintf("%s_cache_hits", name))
	if err != nil {
		return nil, errors.Wrap(err, "creating cache hit counter")
	}

	cacheMissCounter, err := mp.NewInt64Counter(fmt.Sprintf("%s_cache_misses", name))
	if err != nil {
		return nil, errors.Wrap(err, "creating cache miss counter")
	}

	cacheSetCounter, err := mp.NewInt64Counter(fmt.Sprintf("%s_cache_sets", name))
	if err != nil {
		return nil, errors.Wrap(err, "creating cache set counter")
	}

	cacheDelCounter, err := mp.NewInt64Counter(fmt.Sprintf("%s_cache_deletes", name))
	if err != nil {
		return nil, errors.Wrap(err, "creating cache delete counter")
	}

	cacheErrCounter, err := mp.NewInt64Counter(fmt.Sprintf("%s_cache_errors", name))
	if err != nil {
		return nil, errors.Wrap(err, "creating cache error counter")
	}

	latencyHist, err := mp.NewFloat64Histogram(fmt.Sprintf("%s_cache_latency_ms", name))
	if err != nil {
		return nil, errors.Wrap(err, "creating cache latency histogram")
	}

	return &redisCacheImpl[T]{
		logger:           logging.NewNamedLogger(logger, name),
		tracer:           tracing.NewNamedTracer(tracerProvider, name),
		cacheHitCounter:  cacheHitCounter,
		cacheMissCounter: cacheMissCounter,
		cacheSetCounter:  cacheSetCounter,
		cacheDelCounter:  cacheDelCounter,
		cacheErrCounter:  cacheErrCounter,
		latencyHist:      latencyHist,
		client:           buildRedisClient(cfg),
		circuitBreaker:   circuitbreakingcfg.EnsureCircuitBreaker(cb),
		expiration:       expiration,
	}, nil
}

func (i *redisCacheImpl[T]) Get(ctx context.Context, key string) (*T, error) {
	_, span := i.tracer.StartSpan(ctx)
	defer span.End()

	if i.circuitBreaker.CannotProceed() {
		i.cacheMissCounter.Add(ctx, 1)
		return nil, cache.ErrNotFound
	}

	startTime := time.Now()
	defer func() {
		i.latencyHist.Record(ctx, float64(time.Since(startTime).Milliseconds()))
	}()

	res, err := i.client.Get(ctx, key).Result()
	if err != nil {
		i.logger.Error("getting from cache", err)
		i.cacheErrCounter.Add(ctx, 1)
		i.circuitBreaker.Failed()
		return nil, err
	}

	b := strings.NewReader(res)

	var x *T
	if err = gob.NewDecoder(b).Decode(&x); err != nil {
		i.cacheErrCounter.Add(ctx, 1)
		return nil, errors.Wrap(err, "decoding from cache")
	}

	if x == nil {
		i.cacheMissCounter.Add(ctx, 1)
		return nil, cache.ErrNotFound
	}

	i.circuitBreaker.Succeeded()
	i.cacheHitCounter.Add(ctx, 1)

	return x, nil
}

func (i *redisCacheImpl[T]) Set(ctx context.Context, key string, value *T) error {
	_, span := i.tracer.StartSpan(ctx)
	defer span.End()

	if i.circuitBreaker.CannotProceed() {
		return nil
	}

	startTime := time.Now()
	defer func() {
		i.latencyHist.Record(ctx, float64(time.Since(startTime).Milliseconds()))
	}()

	var b bytes.Buffer
	if err := gob.NewEncoder(&b).Encode(value); err != nil {
		i.cacheErrCounter.Add(ctx, 1)
		return errors.Wrap(err, "encoding for cache")
	}

	if setErr := i.client.Set(ctx, key, b.String(), i.expiration).Err(); setErr != nil {
		i.cacheErrCounter.Add(ctx, 1)
		i.circuitBreaker.Failed()
		return setErr
	}

	i.circuitBreaker.Succeeded()
	i.cacheSetCounter.Add(ctx, 1)

	return nil
}

func (i *redisCacheImpl[T]) Delete(ctx context.Context, key string) error {
	_, span := i.tracer.StartSpan(ctx)
	defer span.End()

	if i.circuitBreaker.CannotProceed() {
		return nil
	}

	startTime := time.Now()
	defer func() {
		i.latencyHist.Record(ctx, float64(time.Since(startTime).Milliseconds()))
	}()

	if err := i.client.Del(ctx, key).Err(); err != nil {
		i.cacheErrCounter.Add(ctx, 1)
		i.circuitBreaker.Failed()
		return err
	}

	i.circuitBreaker.Succeeded()
	i.cacheDelCounter.Add(ctx, 1)

	return nil
}

func (i *redisCacheImpl[T]) Ping(ctx context.Context) error {
	return i.client.Ping(ctx).Err()
}

// buildRedisClient returns a PublisherProvider for a given address.
func buildRedisClient(cfg *Config) redisClient {
	var c redisClient
	if len(cfg.QueueAddresses) > 1 {
		c = redis.NewClusterClient(&redis.ClusterOptions{
			Addrs:        cfg.QueueAddresses,
			Username:     cfg.Username,
			Password:     cfg.Password,
			DialTimeout:  1 * time.Second,
			ReadTimeout:  1 * time.Second,
			WriteTimeout: 1 * time.Second,
		})
	} else if len(cfg.QueueAddresses) == 1 {
		c = redis.NewClient(&redis.Options{
			Addr:         cfg.QueueAddresses[0],
			Username:     cfg.Username,
			Password:     cfg.Password,
			DialTimeout:  1 * time.Second,
			ReadTimeout:  1 * time.Second,
			WriteTimeout: 1 * time.Second,
		})
	}

	return c
}
