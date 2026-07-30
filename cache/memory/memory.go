package memory

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/primandproper/platform-go/v8/cache"
	"github.com/primandproper/platform-go/v8/clock"
	"github.com/primandproper/platform-go/v8/errors"
	"github.com/primandproper/platform-go/v8/observability"
	"github.com/primandproper/platform-go/v8/observability/logging"
	"github.com/primandproper/platform-go/v8/observability/metrics"
	"github.com/primandproper/platform-go/v8/observability/tracing"
)

const name = "in_memory_cache"

var _ cache.Cache[struct{}] = (*inMemoryCacheImpl[struct{}])(nil)

// entry is one stored value; a zero expiresAt means the entry never expires.
type entry[T any] struct {
	expiresAt time.Time
	value     *T
}

type inMemoryCacheImpl[T any] struct {
	o11y              observability.Observer
	logger            logging.Logger
	tracerProvider    tracing.TracerProvider
	metricsProvider   metrics.Provider
	clock             clock.Clock
	janitor           func()
	cacheHitCounter   metrics.Int64Counter
	cacheMissCounter  metrics.Int64Counter
	cacheSetCounter   metrics.Int64Counter
	cacheDelCounter   metrics.Int64Counter
	cacheEvictCounter metrics.Int64Counter
	latencyHist       metrics.Float64Histogram
	cache             map[string]entry[T]
	defaultExpiry     time.Duration
	cacheMu           sync.RWMutex
}

// NewInMemoryCache builds an in-memory cache. Writes expire after defaultExpiry
// unless overridden per call with cache.WithExpiry; a non-positive defaultExpiry
// means entries never expire by default.
//
// By default expired entries are evicted lazily, on the read that discovers
// them or when overwritten, and the map is not otherwise size-bounded. Pass
// WithJanitor to sweep them on a timer instead — see that option for when the
// lazy default is not enough.
func NewInMemoryCache[T any](defaultExpiry time.Duration, opts ...Option) (cache.Cache[T], error) {
	o := &options{}
	for _, opt := range opts {
		if opt != nil {
			opt(o)
		}
	}

	i := &inMemoryCacheImpl[T]{
		clock:           clock.NewClock(),
		cache:           make(map[string]entry[T]),
		defaultExpiry:   defaultExpiry,
		logger:          o.logger,
		tracerProvider:  o.tracerProvider,
		metricsProvider: o.metricsProvider,
	}

	// Staged, not started: the sweep must not observe a half-built cache, so it
	// is launched at the end of construction once the counters exist.
	if o.janitorCtx != nil && o.janitorInterval > 0 {
		i.janitor = func() { go i.sweepEvery(o.janitorCtx, o.janitorInterval) }
	}

	i.o11y = observability.NewObserver(name, i.logger, i.tracerProvider)

	mp := metrics.EnsureMetricsProvider(i.metricsProvider)

	var err error

	i.cacheHitCounter, err = mp.NewInt64Counter(fmt.Sprintf("%s_cache_hits", name))
	if err != nil {
		return nil, errors.Wrap(err, "creating cache hit counter")
	}

	i.cacheMissCounter, err = mp.NewInt64Counter(fmt.Sprintf("%s_cache_misses", name))
	if err != nil {
		return nil, errors.Wrap(err, "creating cache miss counter")
	}

	i.cacheSetCounter, err = mp.NewInt64Counter(fmt.Sprintf("%s_cache_sets", name))
	if err != nil {
		return nil, errors.Wrap(err, "creating cache set counter")
	}

	i.cacheDelCounter, err = mp.NewInt64Counter(fmt.Sprintf("%s_cache_deletes", name))
	if err != nil {
		return nil, errors.Wrap(err, "creating cache delete counter")
	}

	i.cacheEvictCounter, err = mp.NewInt64Counter(fmt.Sprintf("%s_cache_evictions", name))
	if err != nil {
		return nil, errors.Wrap(err, "creating cache eviction counter")
	}

	i.latencyHist, err = mp.NewFloat64Histogram(fmt.Sprintf("%s_cache_latency_ms", name))
	if err != nil {
		return nil, errors.Wrap(err, "creating cache latency histogram")
	}

	// Started last, so the sweep can never observe a partially built cache.
	if i.janitor != nil {
		i.janitor()
	}

	return i, nil
}

// expired reports whether e's deadline has passed. A zero deadline never
// expires.
func (i *inMemoryCacheImpl[T]) expired(e entry[T]) bool {
	return !e.expiresAt.IsZero() && !i.clock.Now().Before(e.expiresAt)
}

// newEntry stamps value with the deadline resolved from this call's options.
func (i *inMemoryCacheImpl[T]) newEntry(value *T, opts []cache.WriteOption) entry[T] {
	e := entry[T]{value: value}
	if expiry := cache.EffectiveExpiry(i.defaultExpiry, opts...); expiry > 0 {
		e.expiresAt = i.clock.Now().Add(expiry)
	}

	return e
}

// evictExpired removes key if it is still present and still expired, so a
// concurrent overwrite between the read lock and this write lock is never
// discarded.
//
// This is the only place an entry is dropped for having expired, so it is the
// only place that counts an eviction. An expired entry that a Set or SetMany
// overwrites before any read discovers it is replaced silently and never
// counted: that is a write, not a TTL loss, and folding the two together would
// make the counter useless for the question it exists to answer.
func (i *inMemoryCacheImpl[T]) evictExpired(ctx context.Context, key string) {
	i.cacheMu.Lock()
	defer i.cacheMu.Unlock()

	if cur, ok := i.cache[key]; ok && i.expired(cur) {
		delete(i.cache, key)
		i.cacheEvictCounter.Add(ctx, 1)
	}
}

func (i *inMemoryCacheImpl[T]) Get(ctx context.Context, key string) (*T, error) {
	ctx, op := i.o11y.Begin(ctx)
	defer op.End()
	op.Set("name", key)

	startTime := time.Now()
	defer func() {
		i.latencyHist.Record(ctx, float64(time.Since(startTime).Milliseconds()))
	}()

	i.cacheMu.RLock()
	e, ok := i.cache[key]
	i.cacheMu.RUnlock()

	if ok && !i.expired(e) {
		i.cacheHitCounter.Add(ctx, 1)
		return e.value, nil
	}

	if ok {
		i.evictExpired(ctx, key)
	}

	i.cacheMissCounter.Add(ctx, 1)

	return nil, cache.ErrNotFound
}

func (i *inMemoryCacheImpl[T]) Set(ctx context.Context, key string, value *T, opts ...cache.WriteOption) error {
	ctx, op := i.o11y.Begin(ctx)
	defer op.End()
	op.Set("name", key)

	startTime := time.Now()
	defer func() {
		i.latencyHist.Record(ctx, float64(time.Since(startTime).Milliseconds()))
	}()

	e := i.newEntry(value, opts)

	i.cacheMu.Lock()
	defer i.cacheMu.Unlock()

	i.cache[key] = e
	i.cacheSetCounter.Add(ctx, 1)

	return nil
}

func (i *inMemoryCacheImpl[T]) Delete(ctx context.Context, key string) error {
	ctx, op := i.o11y.Begin(ctx)
	defer op.End()
	op.Set("name", key)

	startTime := time.Now()
	defer func() {
		i.latencyHist.Record(ctx, float64(time.Since(startTime).Milliseconds()))
	}()

	i.cacheMu.Lock()
	defer i.cacheMu.Unlock()

	delete(i.cache, key)
	i.cacheDelCounter.Add(ctx, 1)

	return nil
}

func (i *inMemoryCacheImpl[T]) GetMany(ctx context.Context, keys []string) (map[string]*T, error) {
	ctx, op := i.o11y.Begin(ctx)
	defer op.End()
	op.Set("length", len(keys))

	startTime := time.Now()
	defer func() {
		i.latencyHist.Record(ctx, float64(time.Since(startTime).Milliseconds()))
	}()

	var expiredKeys []string

	i.cacheMu.RLock()
	out := make(map[string]*T, len(keys))
	for _, key := range keys {
		e, ok := i.cache[key]
		if ok && !i.expired(e) {
			out[key] = e.value
			i.cacheHitCounter.Add(ctx, 1)
			continue
		}

		if ok {
			expiredKeys = append(expiredKeys, key)
		}
		i.cacheMissCounter.Add(ctx, 1)
	}
	i.cacheMu.RUnlock()

	for _, key := range expiredKeys {
		i.evictExpired(ctx, key)
	}

	return out, nil
}

func (i *inMemoryCacheImpl[T]) SetMany(ctx context.Context, items map[string]*T, opts ...cache.WriteOption) error {
	ctx, op := i.o11y.Begin(ctx)
	defer op.End()
	op.Set("length", len(items))

	startTime := time.Now()
	defer func() {
		i.latencyHist.Record(ctx, float64(time.Since(startTime).Milliseconds()))
	}()

	// One deadline for the whole batch: options apply to the call, not per item.
	var expiresAt time.Time
	if expiry := cache.EffectiveExpiry(i.defaultExpiry, opts...); expiry > 0 {
		expiresAt = i.clock.Now().Add(expiry)
	}

	i.cacheMu.Lock()
	defer i.cacheMu.Unlock()

	for key, value := range items {
		i.cache[key] = entry[T]{value: value, expiresAt: expiresAt}
		i.cacheSetCounter.Add(ctx, 1)
	}

	return nil
}

// DeleteMany removes the given keys; keys that are absent are not an error.
func (i *inMemoryCacheImpl[T]) DeleteMany(ctx context.Context, keys []string) error {
	ctx, op := i.o11y.Begin(ctx)
	defer op.End()
	op.Set("length", len(keys))

	startTime := time.Now()
	defer func() {
		i.latencyHist.Record(ctx, float64(time.Since(startTime).Milliseconds()))
	}()

	i.cacheMu.Lock()
	defer i.cacheMu.Unlock()

	for _, key := range keys {
		if _, ok := i.cache[key]; ok {
			delete(i.cache, key)
			i.cacheDelCounter.Add(ctx, 1)
		}
	}

	return nil
}

// DeleteByPrefix removes every entry whose key begins with prefix. The memory
// provider wholly owns its map, so an empty prefix is permitted and clears
// everything.
func (i *inMemoryCacheImpl[T]) DeleteByPrefix(ctx context.Context, prefix string) error {
	ctx, op := i.o11y.Begin(ctx)
	defer op.End()
	op.Set("prefix", prefix)

	startTime := time.Now()
	defer func() {
		i.latencyHist.Record(ctx, float64(time.Since(startTime).Milliseconds()))
	}()

	i.cacheMu.Lock()
	defer i.cacheMu.Unlock()

	for key := range i.cache {
		if strings.HasPrefix(key, prefix) {
			delete(i.cache, key)
			i.cacheDelCounter.Add(ctx, 1)
		}
	}

	return nil
}

// Flush removes every entry. The memory provider wholly owns its store, so no
// namespace is needed.
func (i *inMemoryCacheImpl[T]) Flush(ctx context.Context) error {
	ctx, op := i.o11y.Begin(ctx)
	defer op.End()

	startTime := time.Now()
	defer func() {
		i.latencyHist.Record(ctx, float64(time.Since(startTime).Milliseconds()))
	}()

	i.cacheMu.Lock()
	defer i.cacheMu.Unlock()

	i.cacheDelCounter.Add(ctx, int64(len(i.cache)))
	i.cache = make(map[string]entry[T])

	return nil
}

func (i *inMemoryCacheImpl[T]) Ping(ctx context.Context) error {
	_, op := i.o11y.Begin(ctx)
	defer op.End()

	op.Logger().Debug("ping")

	return nil
}
