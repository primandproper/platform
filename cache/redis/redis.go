package redis

import (
	"context"
	stderrors "errors"
	"fmt"
	"strings"
	"time"

	"github.com/primandproper/platform-go/v9/cache"
	"github.com/primandproper/platform-go/v9/cache/redis/slots"
	"github.com/primandproper/platform-go/v9/circuitbreaking"
	circuitbreakingcfg "github.com/primandproper/platform-go/v9/circuitbreaking/config"
	"github.com/primandproper/platform-go/v9/errors"
	"github.com/primandproper/platform-go/v9/observability"
	"github.com/primandproper/platform-go/v9/observability/logging"
	"github.com/primandproper/platform-go/v9/observability/metrics"
	"github.com/primandproper/platform-go/v9/observability/tracing"

	"github.com/redis/go-redis/v9"
)

const name = "redis_cache"

// defaultScanPageSize bounds how many keys a single SCAN iteration asks for
// during prefix deletion, when WithScanPageSize is not supplied.
const defaultScanPageSize = 1000

// batchSetScript stores every KEYS[i] with the value at ARGV[i+1], applying a
// single millisecond TTL (ARGV[1]) to all of them in one round trip. Vanilla
// MSET cannot attach a TTL, so the writes and their expiry are issued together
// inside the script. A non-positive TTL stores the value without expiry, matching
// go-redis' Set semantics for a zero expiration.
const batchSetScript = `
local ttl = tonumber(ARGV[1])
for i = 1, #KEYS do
    if ttl > 0 then
        redis.call('SET', KEYS[i], ARGV[i + 1], 'PX', ttl)
    else
        redis.call('SET', KEYS[i], ARGV[i + 1])
    end
end
return #KEYS
`

var _ cache.Cache[struct{}] = (*redisCacheImpl[struct{}])(nil)

// ErrCodecTypeMismatch indicates WithCodec was given a codec for a type other
// than the cache's. Option carries no type parameter, so the compiler cannot
// catch this; NewRedisCache reports it instead.
var ErrCodecTypeMismatch = errors.New("codec type does not match cache type")

// scanDelClient is the slice of redisClient that prefix deletion needs; it is
// satisfied by both the cache's own client and the per-master *redis.Client
// handles ForEachMaster yields in cluster mode. redisClient embeds it so the
// two cannot drift.
type scanDelClient interface {
	Scan(ctx context.Context, cursor uint64, match string, count int64) *redis.ScanCmd
	Del(ctx context.Context, keys ...string) *redis.IntCmd
}

type redisClient interface {
	scanDelClient

	Get(ctx context.Context, key string) *redis.StringCmd
	Set(ctx context.Context, key string, value any, expiration time.Duration) *redis.StatusCmd
	MGet(ctx context.Context, keys ...string) *redis.SliceCmd
	Eval(ctx context.Context, script string, keys []string, args ...any) *redis.Cmd
	Ping(ctx context.Context) *redis.StatusCmd
	Close() error
}

type redisCacheImpl[T any] struct {
	o11y             observability.Observer
	logger           logging.Logger
	tracerProvider   tracing.TracerProvider
	metricsProvider  metrics.Provider
	codec            cache.Codec[T]
	cacheHitCounter  metrics.Int64Counter
	cacheMissCounter metrics.Int64Counter
	cacheSetCounter  metrics.Int64Counter
	cacheDelCounter  metrics.Int64Counter
	cacheErrCounter  metrics.Int64Counter
	latencyHist      metrics.Float64Histogram
	client           redisClient
	circuitBreaker   circuitbreaking.CircuitBreaker
	namespace        string
	expiration       time.Duration
	scanPageSize     int64
	isCluster        bool
}

// NewRedisCache builds a new redis-backed cache. When cfg.Namespace is set,
// every key is transparently prefixed with it: callers always use bare keys,
// the namespace marks which entries this cache owns, and Flush becomes
// possible (it deletes exactly the namespace's keys). Without a namespace,
// Flush and an empty-prefix DeleteByPrefix return cache.ErrNamespaceRequired
// rather than guess at ownership in a possibly shared database.
func NewRedisCache[T any](cfg *Config, expiration time.Duration, cb circuitbreaking.CircuitBreaker, opts ...Option) (cache.Cache[T], error) {
	if cfg == nil || len(cfg.QueueAddresses) == 0 {
		return nil, fmt.Errorf("at least one redis address is required")
	}

	o := &options{scanPageSize: defaultScanPageSize}
	for _, opt := range opts {
		if opt != nil {
			opt(o)
		}
	}

	impl := &redisCacheImpl[T]{
		codec:           cache.NewGobCodec[T](),
		circuitBreaker:  circuitbreakingcfg.EnsureCircuitBreaker(cb),
		namespace:       cfg.Namespace,
		expiration:      expiration,
		scanPageSize:    o.scanPageSize,
		isCluster:       cfg.clusterMode(),
		logger:          o.logger,
		tracerProvider:  o.tracerProvider,
		metricsProvider: o.metricsProvider,
	}

	// Asserted rather than assumed: Option cannot name T, so this is where a
	// codec built for another type is caught. Silently keeping the gob default
	// would be worse than failing — the cache would encode correctly and the
	// caller would never learn their codec was ignored.
	if o.codec != nil {
		codec, ok := o.codec.(cache.Codec[T])
		if !ok {
			return nil, errors.Wrapf(
				ErrCodecTypeMismatch, "codec is %T, want cache.Codec[%T]", o.codec, *new(T),
			)
		}

		impl.codec = codec
	}

	impl.o11y = observability.NewObserver(name, impl.logger, impl.tracerProvider)

	mp := metrics.EnsureMetricsProvider(impl.metricsProvider)

	var err error

	impl.cacheHitCounter, err = mp.NewInt64Counter(fmt.Sprintf("%s_cache_hits", name))
	if err != nil {
		return nil, errors.Wrap(err, "creating cache hit counter")
	}

	impl.cacheMissCounter, err = mp.NewInt64Counter(fmt.Sprintf("%s_cache_misses", name))
	if err != nil {
		return nil, errors.Wrap(err, "creating cache miss counter")
	}

	impl.cacheSetCounter, err = mp.NewInt64Counter(fmt.Sprintf("%s_cache_sets", name))
	if err != nil {
		return nil, errors.Wrap(err, "creating cache set counter")
	}

	impl.cacheDelCounter, err = mp.NewInt64Counter(fmt.Sprintf("%s_cache_deletes", name))
	if err != nil {
		return nil, errors.Wrap(err, "creating cache delete counter")
	}

	impl.cacheErrCounter, err = mp.NewInt64Counter(fmt.Sprintf("%s_cache_errors", name))
	if err != nil {
		return nil, errors.Wrap(err, "creating cache error counter")
	}

	impl.latencyHist, err = mp.NewFloat64Histogram(fmt.Sprintf("%s_cache_latency_ms", name))
	if err != nil {
		return nil, errors.Wrap(err, "creating cache latency histogram")
	}

	// Built last, so a failed constructor never leaves a connected client behind.
	impl.client = buildRedisClient(cfg)

	return impl, nil
}

// key returns the stored form of a caller key: the configured namespace
// prepended. Every operation goes through this, so callers only ever see bare
// keys.
func (i *redisCacheImpl[T]) key(k string) string {
	return i.namespace + k
}

func (i *redisCacheImpl[T]) Get(ctx context.Context, key string) (*T, error) {
	ctx, op := i.o11y.Begin(ctx, observability.WithValue("name", key))
	defer op.End()

	if i.circuitBreaker.CannotProceed() {
		i.cacheMissCounter.Add(ctx, 1)
		return nil, cache.ErrNotFound
	}

	startTime := time.Now()
	defer func() {
		i.latencyHist.Record(ctx, float64(time.Since(startTime).Milliseconds()))
	}()

	res, err := i.client.Get(ctx, i.key(key)).Result()
	if err != nil {
		// A key miss is a healthy response, not an infrastructure failure: don't count it
		// as an error or trip the breaker, and surface the sentinel callers check for.
		if stderrors.Is(err, redis.Nil) {
			i.circuitBreaker.Succeeded()
			i.cacheMissCounter.Add(ctx, 1)
			return nil, cache.ErrNotFound
		}

		i.cacheErrCounter.Add(ctx, 1)
		i.circuitBreaker.Failed()
		return nil, op.Error(err, "getting from cache")
	}

	x, err := i.decode(res)
	if err != nil {
		i.cacheErrCounter.Add(ctx, 1)
		return nil, op.Error(err, "decoding cached value")
	}

	if x == nil {
		i.cacheMissCounter.Add(ctx, 1)
		return nil, cache.ErrNotFound
	}

	i.circuitBreaker.Succeeded()
	i.cacheHitCounter.Add(ctx, 1)

	return x, nil
}

func (i *redisCacheImpl[T]) Set(ctx context.Context, key string, value *T, opts ...cache.WriteOption) error {
	ctx, op := i.o11y.Begin(ctx, observability.WithValue("name", key))
	defer op.End()

	if i.circuitBreaker.CannotProceed() {
		return nil
	}

	startTime := time.Now()
	defer func() {
		i.latencyHist.Record(ctx, float64(time.Since(startTime).Milliseconds()))
	}()

	encoded, err := i.encode(value)
	if err != nil {
		i.cacheErrCounter.Add(ctx, 1)
		return op.Error(err, "encoding value for cache")
	}

	if setErr := i.client.Set(ctx, i.key(key), encoded, cache.EffectiveExpiry(i.expiration, opts...)).Err(); setErr != nil {
		i.cacheErrCounter.Add(ctx, 1)
		i.circuitBreaker.Failed()
		return setErr
	}

	i.circuitBreaker.Succeeded()
	i.cacheSetCounter.Add(ctx, 1)

	return nil
}

func (i *redisCacheImpl[T]) Delete(ctx context.Context, key string) error {
	ctx, op := i.o11y.Begin(ctx, observability.WithValue("name", key))
	defer op.End()

	if i.circuitBreaker.CannotProceed() {
		return nil
	}

	startTime := time.Now()
	defer func() {
		i.latencyHist.Record(ctx, float64(time.Since(startTime).Milliseconds()))
	}()

	if err := i.client.Del(ctx, i.key(key)).Err(); err != nil {
		i.cacheErrCounter.Add(ctx, 1)
		i.circuitBreaker.Failed()
		return err
	}

	i.circuitBreaker.Succeeded()
	i.cacheDelCounter.Add(ctx, 1)

	return nil
}

// DeleteMany removes the given keys. In cluster mode a multi-key DEL requires
// every key to share a hash slot, so the keys are bucketed by slot and
// deleted one DEL per slot; a single-node client deletes them in one DEL.
func (i *redisCacheImpl[T]) DeleteMany(ctx context.Context, keys []string) error {
	ctx, op := i.o11y.Begin(ctx, observability.WithValue("length", len(keys)))
	defer op.End()

	if len(keys) == 0 {
		return nil
	}

	if i.circuitBreaker.CannotProceed() {
		return nil
	}

	startTime := time.Now()
	defer func() {
		i.latencyHist.Record(ctx, float64(time.Since(startTime).Milliseconds()))
	}()

	stored := make([]string, len(keys))
	for idx, k := range keys {
		stored[idx] = i.key(k)
	}

	for _, group := range i.slotGroups(stored) {
		if len(group) == 0 {
			continue
		}

		deleted, err := i.client.Del(ctx, group...).Result()
		if err != nil {
			i.cacheErrCounter.Add(ctx, 1)
			i.circuitBreaker.Failed()
			return op.Error(err, "deleting many from cache")
		}

		i.cacheDelCounter.Add(ctx, deleted)
	}

	i.circuitBreaker.Succeeded()

	return nil
}

// DeleteByPrefix removes every entry whose (caller-visible) key begins with
// prefix, via a cursor SCAN over the namespaced pattern. Without a configured
// namespace an empty prefix is refused with cache.ErrNamespaceRequired —
// matching every key in a possibly shared database is not ownership.
func (i *redisCacheImpl[T]) DeleteByPrefix(ctx context.Context, prefix string) error {
	ctx, op := i.o11y.Begin(ctx, observability.WithValue("prefix", prefix))
	defer op.End()

	if i.namespace == "" && prefix == "" {
		return cache.ErrNamespaceRequired
	}

	if i.circuitBreaker.CannotProceed() {
		return nil
	}

	startTime := time.Now()
	defer func() {
		i.latencyHist.Record(ctx, float64(time.Since(startTime).Milliseconds()))
	}()

	pattern := escapeGlob(i.key(prefix)) + "*"

	deleted, err := i.deleteByPattern(ctx, pattern)
	i.cacheDelCounter.Add(ctx, deleted)
	if err != nil {
		i.cacheErrCounter.Add(ctx, 1)
		i.circuitBreaker.Failed()
		return op.Error(err, "deleting by prefix from cache")
	}

	i.circuitBreaker.Succeeded()

	return nil
}

// Flush removes every entry this cache owns. Ownership is the configured
// namespace; without one this cache cannot distinguish its entries in a
// possibly shared database, and Flush returns cache.ErrNamespaceRequired
// rather than reach for FLUSHDB.
func (i *redisCacheImpl[T]) Flush(ctx context.Context) error {
	if i.namespace == "" {
		return cache.ErrNamespaceRequired
	}

	return i.DeleteByPrefix(ctx, "")
}

// deleteByPattern scans for pattern and deletes what it finds. SCAN is
// per-node, so in cluster mode every master is scanned; on a single node the
// cache's own client is scanned directly.
func (i *redisCacheImpl[T]) deleteByPattern(ctx context.Context, pattern string) (int64, error) {
	if clusterClient, ok := i.client.(*redis.ClusterClient); ok && i.isCluster {
		var total int64
		err := clusterClient.ForEachMaster(ctx, func(ctx context.Context, master *redis.Client) error {
			n, scanErr := i.scanAndDelete(ctx, master, pattern)
			total += n
			return scanErr
		})

		return total, err
	}

	return i.scanAndDelete(ctx, i.client, pattern)
}

// scanAndDelete drives one client's cursor SCAN over pattern, deleting each
// page. Pages are slot-grouped before DEL so cluster masters never see a
// cross-slot multi-key command.
func (i *redisCacheImpl[T]) scanAndDelete(ctx context.Context, c scanDelClient, pattern string) (int64, error) {
	var (
		deleted int64
		cursor  uint64
	)

	for {
		keys, next, err := c.Scan(ctx, cursor, pattern, i.scanPageSize).Result()
		if err != nil {
			return deleted, errors.Wrap(err, "scanning for keys")
		}

		for _, group := range i.slotGroups(keys) {
			if len(group) == 0 {
				continue
			}

			n, delErr := c.Del(ctx, group...).Result()
			deleted += n
			if delErr != nil {
				return deleted, errors.Wrap(delErr, "deleting scanned keys")
			}
		}

		cursor = next
		if cursor == 0 {
			return deleted, nil
		}
	}
}

func (i *redisCacheImpl[T]) Ping(ctx context.Context) error {
	return i.client.Ping(ctx).Err()
}

// GetMany fetches multiple keys, returning only those that were present. In
// cluster mode MGET requires every key to share a hash slot, so the keys are
// bucketed by slot and fetched one MGET per slot; a single-node client fetches
// them all in one MGET. Results are keyed by the caller's bare keys.
func (i *redisCacheImpl[T]) GetMany(ctx context.Context, keys []string) (map[string]*T, error) {
	ctx, op := i.o11y.Begin(ctx, observability.WithValue("length", len(keys)))
	defer op.End()

	out := make(map[string]*T, len(keys))
	if len(keys) == 0 {
		return out, nil
	}

	if i.circuitBreaker.CannotProceed() {
		i.cacheMissCounter.Add(ctx, int64(len(keys)))
		return out, nil
	}

	startTime := time.Now()
	defer func() {
		i.latencyHist.Record(ctx, float64(time.Since(startTime).Milliseconds()))
	}()

	stored := make([]string, len(keys))
	callerKey := make(map[string]string, len(keys))
	for idx, k := range keys {
		sk := i.key(k)
		stored[idx] = sk
		callerKey[sk] = k
	}

	for _, group := range i.slotGroups(stored) {
		values, err := i.client.MGet(ctx, group...).Result()
		if err != nil {
			i.cacheErrCounter.Add(ctx, 1)
			i.circuitBreaker.Failed()
			return nil, op.Error(err, "getting many from cache")
		}

		for idx, v := range values {
			s, ok := v.(string)
			if !ok {
				// A nil element (or any non-string) is a missing key.
				i.cacheMissCounter.Add(ctx, 1)
				continue
			}

			decoded, decodeErr := i.decode(s)
			if decodeErr != nil {
				i.cacheErrCounter.Add(ctx, 1)
				return nil, op.Error(decodeErr, "decoding cached value")
			}

			if decoded == nil {
				i.cacheMissCounter.Add(ctx, 1)
				continue
			}

			out[callerKey[group[idx]]] = decoded
			i.cacheHitCounter.Add(ctx, 1)
		}
	}

	i.circuitBreaker.Succeeded()

	return out, nil
}

// SetMany stores multiple values, each with the expiration resolved from this
// call's options (the cache's configured default when none are given). The
// writes and their expiry are applied together inside a single Lua script
// (see batchSetScript), which is both atomic and a single round trip. In cluster
// mode EVAL requires every key to share a hash slot, so the batch is split per
// slot.
func (i *redisCacheImpl[T]) SetMany(ctx context.Context, items map[string]*T, opts ...cache.WriteOption) error {
	ctx, op := i.o11y.Begin(ctx, observability.WithValue("length", len(items)))
	defer op.End()

	if len(items) == 0 {
		return nil
	}

	if i.circuitBreaker.CannotProceed() {
		return nil
	}

	startTime := time.Now()
	defer func() {
		i.latencyHist.Record(ctx, float64(time.Since(startTime).Milliseconds()))
	}()

	// Encode every value first so a single bad value fails the batch before any
	// write is issued.
	encoded := make(map[string]string, len(items))
	stored := make([]string, 0, len(items))
	for key, value := range items {
		s, err := i.encode(value)
		if err != nil {
			i.cacheErrCounter.Add(ctx, 1)
			return op.Error(err, "encoding value for cache")
		}

		sk := i.key(key)
		encoded[sk] = s
		stored = append(stored, sk)
	}

	expiry := cache.EffectiveExpiry(i.expiration, opts...).Milliseconds()
	for _, group := range i.slotGroups(stored) {
		args := make([]any, 0, len(group)+1)
		args = append(args, expiry)
		for _, key := range group {
			args = append(args, encoded[key])
		}

		if err := i.client.Eval(ctx, batchSetScript, group, args...).Err(); err != nil {
			i.cacheErrCounter.Add(ctx, 1)
			i.circuitBreaker.Failed()
			return err
		}
	}

	i.circuitBreaker.Succeeded()
	i.cacheSetCounter.Add(ctx, int64(len(stored)))

	return nil
}

// slotGroups splits keys into batches that are safe for a single
// MGET/EVAL/DEL. A single-node client has no hash-slot restriction, so all
// keys go in one group; a cluster client requires every key in a call to map
// to the same slot, so the keys are bucketed by slot.
func (i *redisCacheImpl[T]) slotGroups(keys []string) [][]string {
	if !i.isCluster {
		return [][]string{keys}
	}

	return groupBySlot(keys)
}

// groupBySlot buckets keys by their Redis Cluster hash slot, reusing the same
// hashtag-aware slot computation the cluster itself applies.
func groupBySlot(keys []string) [][]string {
	bySlot := make(map[uint16][]string)
	for _, key := range keys {
		slot := slots.SlotForKey(key)
		bySlot[slot] = append(bySlot[slot], key)
	}

	groups := make([][]string, 0, len(bySlot))
	for _, group := range bySlot {
		groups = append(groups, group)
	}

	return groups
}

// escapeGlob backslash-escapes SCAN MATCH glob metacharacters so a literal
// prefix containing *, ?, [, ], or \ matches itself rather than acting as a
// pattern.
func escapeGlob(s string) string {
	if !strings.ContainsAny(s, `*?[]\`) {
		return s
	}

	var b strings.Builder
	b.Grow(len(s) + 4)
	for _, r := range s {
		switch r {
		case '*', '?', '[', ']', '\\':
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}

	return b.String()
}

// encode runs the configured codec, yielding the string form stored in Redis.
func (i *redisCacheImpl[T]) encode(value *T) (string, error) {
	b, err := i.codec.Encode(value)
	if err != nil {
		return "", errors.Wrap(err, "encoding for cache")
	}

	return string(b), nil
}

// decode reverses encode through the configured codec.
func (i *redisCacheImpl[T]) decode(s string) (*T, error) {
	x, err := i.codec.Decode([]byte(s))
	if err != nil {
		return nil, errors.Wrap(err, "decoding from cache")
	}

	return x, nil
}

// buildRedisClient returns a PublisherProvider for a given address.
func buildRedisClient(cfg *Config) redisClient {
	var c redisClient
	if cfg.clusterMode() {
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

// Close releases the connection pool. It does not evict anything: the entries
// live in redis and outlive any one client.
//
// It is safe to call more than once — go-redis's Close is idempotent.
func (c *redisCacheImpl[T]) Close() error {
	if c.client == nil {
		return nil
	}

	return c.client.Close()
}
