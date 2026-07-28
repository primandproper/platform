package redis

import (
	"bytes"
	"context"
	"encoding/gob"
	"errors"
	"testing"
	"time"

	"github.com/primandproper/platform-go/v8/cache"
	mockcircuitbreaking "github.com/primandproper/platform-go/v8/circuitbreaking/mock"
	"github.com/primandproper/platform-go/v8/observability"
	"github.com/primandproper/platform-go/v8/observability/metrics"
	mockmetrics "github.com/primandproper/platform-go/v8/observability/metrics/mock"
	metricsnoop "github.com/primandproper/platform-go/v8/observability/metrics/noop"
	"github.com/primandproper/platform-go/v8/testutils/containers/redistest"

	"github.com/redis/go-redis/v9"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
	"go.opentelemetry.io/otel/metric"
)

const exampleKey = "example"

type example struct {
	Name string `json:"name"`
}

func gobEncodeExample(t *testing.T, e *example) string {
	t.Helper()

	var buf bytes.Buffer
	must.NoError(t, gob.NewEncoder(&buf).Encode(e))

	return buf.String()
}

func buildTestImpl(t *testing.T) (*redisCacheImpl[example], *redisClientMock, *mockcircuitbreaking.CircuitBreakerMock, *observability.RecordingObserver) {
	t.Helper()

	mp := metricsnoop.NewMetricsProvider()

	hitCounter, err := mp.NewInt64Counter("test_hits")
	must.NoError(t, err)

	missCounter, err := mp.NewInt64Counter("test_misses")
	must.NoError(t, err)

	setCounter, err := mp.NewInt64Counter("test_sets")
	must.NoError(t, err)

	delCounter, err := mp.NewInt64Counter("test_deletes")
	must.NoError(t, err)

	errCounter, err := mp.NewInt64Counter("test_errors")
	must.NoError(t, err)

	latencyHist, err := mp.NewFloat64Histogram("test_latency")
	must.NoError(t, err)

	client := &redisClientMock{}
	cb := &mockcircuitbreaking.CircuitBreakerMock{}
	obs := observability.NewRecordingObserver()

	return &redisCacheImpl[example]{
		o11y:             obs,
		codec:            cache.NewGobCodec[example](),
		cacheHitCounter:  hitCounter,
		cacheMissCounter: missCounter,
		cacheSetCounter:  setCounter,
		cacheDelCounter:  delCounter,
		cacheErrCounter:  errCounter,
		latencyHist:      latencyHist,
		client:           client,
		circuitBreaker:   cb,
		expiration:       time.Minute,
		scanPageSize:     defaultScanPageSize,
	}, client, cb, obs
}

// counterResult bundles the values a mocked NewInt64Counter call returns.
type counterResult struct {
	counter metrics.Int64Counter
	err     error
}

// newCounterProviderMock returns a metrics.Provider mock whose NewInt64Counter
// implementation looks up the result keyed on the counter name. Unknown names
// fail the test.
func newCounterProviderMock(t *testing.T, results map[string]counterResult) *mockmetrics.ProviderMock {
	t.Helper()
	return &mockmetrics.ProviderMock{
		NewInt64CounterFunc: func(metricName string, _ ...metric.Int64CounterOption) (metrics.Int64Counter, error) {
			res, ok := results[metricName]
			if !ok {
				t.Fatalf("unexpected NewInt64Counter call: %q", metricName)
			}
			return res.counter, res.err
		},
	}
}

func buildContainerBackedRedisConfig(t *testing.T) *Config {
	t.Helper()

	container := redistest.Start(t)
	return &Config{
		QueueAddresses: []string{redistest.Address(t, container)},
	}
}

func TestNewRedisCache(T *testing.T) {
	T.Parallel()

	okCounter := func() metrics.Int64Counter { return metrics.Int64CounterForTest(T, "x") }

	T.Run("with no addresses", func(t *testing.T) {
		t.Parallel()

		c, err := NewRedisCache[example](&Config{}, time.Minute, nil, nil, nil, nil)
		test.Error(t, err)
		test.Nil(t, c)
	})

	T.Run("with nil config", func(t *testing.T) {
		t.Parallel()

		c, err := NewRedisCache[example](nil, time.Minute, nil, nil, nil, nil)
		test.Error(t, err)
		test.Nil(t, c)
	})

	T.Run("with single address", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{QueueAddresses: []string{"localhost:6379"}}

		c, err := NewRedisCache[example](cfg, time.Minute, nil, nil, nil, nil)
		must.NoError(t, err)
		test.NotNil(t, c)
	})

	T.Run("with multiple addresses", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{QueueAddresses: []string{"localhost:6379", "localhost:6380"}}

		c, err := NewRedisCache[example](cfg, time.Minute, nil, nil, nil, nil)
		must.NoError(t, err)
		test.NotNil(t, c)
	})

	T.Run("with error creating cache hit counter", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{QueueAddresses: []string{"localhost:6379"}}

		mp := newCounterProviderMock(t, map[string]counterResult{
			name + "_cache_hits": {counter: okCounter(), err: errors.New("counter error")},
		})

		c, err := NewRedisCache[example](cfg, time.Minute, nil, nil, mp, nil)
		test.Error(t, err)
		test.Nil(t, c)
		test.SliceLen(t, 1, mp.NewInt64CounterCalls())
	})

	T.Run("with error creating cache miss counter", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{QueueAddresses: []string{"localhost:6379"}}

		mp := newCounterProviderMock(t, map[string]counterResult{
			name + "_cache_hits":   {counter: okCounter()},
			name + "_cache_misses": {counter: okCounter(), err: errors.New("counter error")},
		})

		c, err := NewRedisCache[example](cfg, time.Minute, nil, nil, mp, nil)
		test.Error(t, err)
		test.Nil(t, c)
		test.SliceLen(t, 2, mp.NewInt64CounterCalls())
	})

	T.Run("with error creating cache set counter", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{QueueAddresses: []string{"localhost:6379"}}

		mp := newCounterProviderMock(t, map[string]counterResult{
			name + "_cache_hits":   {counter: okCounter()},
			name + "_cache_misses": {counter: okCounter()},
			name + "_cache_sets":   {counter: okCounter(), err: errors.New("counter error")},
		})

		c, err := NewRedisCache[example](cfg, time.Minute, nil, nil, mp, nil)
		test.Error(t, err)
		test.Nil(t, c)
		test.SliceLen(t, 3, mp.NewInt64CounterCalls())
	})

	T.Run("with error creating cache delete counter", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{QueueAddresses: []string{"localhost:6379"}}

		mp := newCounterProviderMock(t, map[string]counterResult{
			name + "_cache_hits":    {counter: okCounter()},
			name + "_cache_misses":  {counter: okCounter()},
			name + "_cache_sets":    {counter: okCounter()},
			name + "_cache_deletes": {counter: okCounter(), err: errors.New("counter error")},
		})

		c, err := NewRedisCache[example](cfg, time.Minute, nil, nil, mp, nil)
		test.Error(t, err)
		test.Nil(t, c)
		test.SliceLen(t, 4, mp.NewInt64CounterCalls())
	})

	T.Run("with error creating cache error counter", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{QueueAddresses: []string{"localhost:6379"}}

		mp := newCounterProviderMock(t, map[string]counterResult{
			name + "_cache_hits":    {counter: okCounter()},
			name + "_cache_misses":  {counter: okCounter()},
			name + "_cache_sets":    {counter: okCounter()},
			name + "_cache_deletes": {counter: okCounter()},
			name + "_cache_errors":  {counter: okCounter(), err: errors.New("counter error")},
		})

		c, err := NewRedisCache[example](cfg, time.Minute, nil, nil, mp, nil)
		test.Error(t, err)
		test.Nil(t, c)
		test.SliceLen(t, 5, mp.NewInt64CounterCalls())
	})

	T.Run("with error creating latency histogram", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{QueueAddresses: []string{"localhost:6379"}}

		noopMP := metricsnoop.NewMetricsProvider()
		h, histErr := noopMP.NewFloat64Histogram("test")
		must.NoError(t, histErr)

		mp := &mockmetrics.ProviderMock{
			NewInt64CounterFunc: func(_ string, _ ...metric.Int64CounterOption) (metrics.Int64Counter, error) {
				return metrics.Int64CounterForTest(t, "x"), nil
			},
			NewFloat64HistogramFunc: func(metricName string, _ ...metric.Float64HistogramOption) (metrics.Float64Histogram, error) {
				test.EqOp(t, name+"_cache_latency_ms", metricName)
				return h, errors.New("histogram error")
			},
		}

		c, err := NewRedisCache[example](cfg, time.Minute, nil, nil, mp, nil)
		test.Error(t, err)
		test.Nil(t, c)
		test.SliceLen(t, 5, mp.NewInt64CounterCalls())
		test.SliceLen(t, 1, mp.NewFloat64HistogramCalls())
	})
}

func Test_redisCacheImpl_Get(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()

		cfg := buildContainerBackedRedisConfig(t)
		c, err := NewRedisCache[example](cfg, 0, nil, nil, nil, nil)
		must.NoError(t, err)

		exampleContent := &example{Name: t.Name()}
		test.NoError(t, c.Set(ctx, exampleKey, exampleContent))

		actual, getErr := c.Get(ctx, exampleKey)
		test.Eq(t, exampleContent, actual)
		test.NoError(t, getErr)
	})
}

func Test_redisCacheImpl_Get_Unit(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		impl, client, cb, obs := buildTestImpl(t)

		expected := &example{Name: t.Name()}
		encoded := gobEncodeExample(t, expected)

		cb.CannotProceedFunc = func() bool { return false }
		cb.SucceededFunc = func() {}

		client.GetFunc = func(_ context.Context, key string) *redis.StringCmd {
			test.EqOp(t, exampleKey, key)
			cmd := redis.NewStringCmd(ctx)
			cmd.SetVal(encoded)
			return cmd
		}

		actual, err := impl.Get(ctx, exampleKey)
		test.NoError(t, err)
		test.Eq(t, expected, actual)

		test.SliceLen(t, 1, client.GetCalls())
		test.SliceLen(t, 1, cb.CannotProceedCalls())
		test.SliceLen(t, 1, cb.SucceededCalls())

		obs.ObservedOperationWithData(t, map[string]any{})
	})

	T.Run("when circuit breaker cannot proceed", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		impl, _, cb, _ := buildTestImpl(t)

		cb.CannotProceedFunc = func() bool { return true }

		actual, err := impl.Get(ctx, exampleKey)
		test.ErrorIs(t, err, cache.ErrNotFound)
		test.Nil(t, actual)

		test.SliceLen(t, 1, cb.CannotProceedCalls())
	})

	T.Run("with cache miss", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		impl, client, cb, obs := buildTestImpl(t)

		cb.CannotProceedFunc = func() bool { return false }
		cb.SucceededFunc = func() {}
		cb.FailedFunc = func() {}

		client.GetFunc = func(_ context.Context, key string) *redis.StringCmd {
			test.EqOp(t, exampleKey, key)
			cmd := redis.NewStringCmd(ctx)
			cmd.SetErr(redis.Nil)
			return cmd
		}

		actual, err := impl.Get(ctx, exampleKey)
		// A miss is the sentinel callers check for, not a wrapped infra error.
		test.ErrorIs(t, err, cache.ErrNotFound)
		test.Nil(t, actual)

		test.SliceLen(t, 1, client.GetCalls())
		// A miss is a healthy response: the breaker records success, not failure.
		test.SliceLen(t, 1, cb.SucceededCalls())
		test.SliceLen(t, 0, cb.FailedCalls())

		// It must not be recorded as an operation error either.
		op := obs.ObservedOperationWithData(t, map[string]any{})
		must.SliceLen(t, 0, op.Errors)
	})

	T.Run("with redis error", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		impl, client, cb, obs := buildTestImpl(t)

		cb.CannotProceedFunc = func() bool { return false }
		cb.FailedFunc = func() {}

		client.GetFunc = func(_ context.Context, key string) *redis.StringCmd {
			test.EqOp(t, exampleKey, key)
			cmd := redis.NewStringCmd(ctx)
			cmd.SetErr(errors.New("redis error"))
			return cmd
		}

		actual, err := impl.Get(ctx, exampleKey)
		test.Error(t, err)
		test.Nil(t, actual)

		test.SliceLen(t, 1, client.GetCalls())
		test.SliceLen(t, 1, cb.FailedCalls())

		// The failure must be recorded on the operation.
		op := obs.ObservedOperationWithData(t, map[string]any{})
		must.SliceLen(t, 1, op.Errors)
	})

	T.Run("with decode error", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		impl, client, cb, _ := buildTestImpl(t)

		cb.CannotProceedFunc = func() bool { return false }

		client.GetFunc = func(_ context.Context, key string) *redis.StringCmd {
			test.EqOp(t, exampleKey, key)
			cmd := redis.NewStringCmd(ctx)
			cmd.SetVal("not valid gob data")
			return cmd
		}

		actual, err := impl.Get(ctx, exampleKey)
		test.Error(t, err)
		test.Nil(t, actual)

		test.SliceLen(t, 1, client.GetCalls())
	})
}

func Test_redisCacheImpl_Set(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()

		cfg := buildContainerBackedRedisConfig(t)
		c, err := NewRedisCache[example](cfg, 0, nil, nil, nil, nil)
		must.NoError(t, err)

		exampleContent := &example{Name: t.Name()}
		test.NoError(t, c.Set(ctx, exampleKey, exampleContent))
	})
}

func Test_redisCacheImpl_Set_Unit(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		impl, client, cb, _ := buildTestImpl(t)

		cb.CannotProceedFunc = func() bool { return false }
		cb.SucceededFunc = func() {}

		client.SetFunc = func(_ context.Context, key string, value any, expiration time.Duration) *redis.StatusCmd {
			test.EqOp(t, exampleKey, key)
			test.EqOp(t, time.Minute, expiration)
			_, isString := value.(string)
			test.True(t, isString)
			cmd := redis.NewStatusCmd(ctx)
			cmd.SetVal("OK")
			return cmd
		}

		err := impl.Set(ctx, exampleKey, &example{Name: t.Name()})
		test.NoError(t, err)

		test.SliceLen(t, 1, client.SetCalls())
		test.SliceLen(t, 1, cb.SucceededCalls())
	})

	T.Run("when circuit breaker cannot proceed", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		impl, _, cb, _ := buildTestImpl(t)

		cb.CannotProceedFunc = func() bool { return true }

		err := impl.Set(ctx, exampleKey, &example{Name: t.Name()})
		test.NoError(t, err)

		test.SliceLen(t, 1, cb.CannotProceedCalls())
	})

	T.Run("with redis error", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		impl, client, cb, _ := buildTestImpl(t)

		cb.CannotProceedFunc = func() bool { return false }
		cb.FailedFunc = func() {}

		client.SetFunc = func(_ context.Context, key string, _ any, _ time.Duration) *redis.StatusCmd {
			test.EqOp(t, exampleKey, key)
			cmd := redis.NewStatusCmd(ctx)
			cmd.SetErr(errors.New("redis error"))
			return cmd
		}

		err := impl.Set(ctx, exampleKey, &example{Name: t.Name()})
		test.Error(t, err)

		test.SliceLen(t, 1, client.SetCalls())
		test.SliceLen(t, 1, cb.FailedCalls())
	})
}

func Test_redisCacheImpl_Delete(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()

		cfg := buildContainerBackedRedisConfig(t)
		c, err := NewRedisCache[example](cfg, 0, nil, nil, nil, nil)
		must.NoError(t, err)

		exampleContent := &example{Name: t.Name()}
		test.NoError(t, c.Set(ctx, exampleKey, exampleContent))

		test.NoError(t, c.Delete(ctx, exampleKey))
	})
}

func Test_redisCacheImpl_Delete_Unit(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		impl, client, cb, _ := buildTestImpl(t)

		cb.CannotProceedFunc = func() bool { return false }
		cb.SucceededFunc = func() {}

		client.DelFunc = func(_ context.Context, keys ...string) *redis.IntCmd {
			test.Eq(t, []string{exampleKey}, keys)
			cmd := redis.NewIntCmd(ctx)
			cmd.SetVal(1)
			return cmd
		}

		err := impl.Delete(ctx, exampleKey)
		test.NoError(t, err)

		test.SliceLen(t, 1, client.DelCalls())
		test.SliceLen(t, 1, cb.SucceededCalls())
	})

	T.Run("when circuit breaker cannot proceed", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		impl, _, cb, _ := buildTestImpl(t)

		cb.CannotProceedFunc = func() bool { return true }

		err := impl.Delete(ctx, exampleKey)
		test.NoError(t, err)

		test.SliceLen(t, 1, cb.CannotProceedCalls())
	})

	T.Run("with redis error", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		impl, client, cb, _ := buildTestImpl(t)

		cb.CannotProceedFunc = func() bool { return false }
		cb.FailedFunc = func() {}

		client.DelFunc = func(_ context.Context, _ ...string) *redis.IntCmd {
			cmd := redis.NewIntCmd(ctx)
			cmd.SetErr(errors.New("redis error"))
			return cmd
		}

		err := impl.Delete(ctx, exampleKey)
		test.Error(t, err)

		test.SliceLen(t, 1, client.DelCalls())
		test.SliceLen(t, 1, cb.FailedCalls())
	})
}

func Test_redisCacheImpl_Ping_Unit(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		impl, client, _, _ := buildTestImpl(t)

		client.PingFunc = func(_ context.Context) *redis.StatusCmd {
			cmd := redis.NewStatusCmd(ctx)
			cmd.SetVal("PONG")
			return cmd
		}

		test.NoError(t, impl.Ping(ctx))
		test.SliceLen(t, 1, client.PingCalls())
	})

	T.Run("with error", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		impl, client, _, _ := buildTestImpl(t)

		client.PingFunc = func(_ context.Context) *redis.StatusCmd {
			cmd := redis.NewStatusCmd(ctx)
			cmd.SetErr(errors.New("connection refused"))
			return cmd
		}

		test.Error(t, impl.Ping(ctx))
		test.SliceLen(t, 1, client.PingCalls())
	})
}

func Test_redisCacheImpl_GetMany_Unit(T *testing.T) {
	T.Parallel()

	T.Run("standard with hit and miss", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		impl, client, cb, obs := buildTestImpl(t)

		found := &example{Name: "found"}
		encoded := gobEncodeExample(t, found)

		cb.CannotProceedFunc = func() bool { return false }
		cb.SucceededFunc = func() {}

		client.MGetFunc = func(_ context.Context, keys ...string) *redis.SliceCmd {
			test.Eq(t, []string{"hit", "miss"}, keys)
			cmd := redis.NewSliceCmd(ctx)
			cmd.SetVal([]any{encoded, nil})
			return cmd
		}

		out, err := impl.GetMany(ctx, []string{"hit", "miss"})
		test.NoError(t, err)
		test.MapLen(t, 1, out)
		test.Eq(t, found, out["hit"])

		test.SliceLen(t, 1, client.MGetCalls())
		test.SliceLen(t, 1, cb.SucceededCalls())

		obs.ObservedOperationWithData(t, map[string]any{})
	})

	T.Run("empty keys short-circuits", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		impl, client, _, _ := buildTestImpl(t)

		out, err := impl.GetMany(ctx, nil)
		test.NoError(t, err)
		test.MapLen(t, 0, out)
		test.SliceLen(t, 0, client.MGetCalls())
	})

	T.Run("when circuit breaker cannot proceed", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		impl, client, cb, _ := buildTestImpl(t)

		cb.CannotProceedFunc = func() bool { return true }

		out, err := impl.GetMany(ctx, []string{"a", "b"})
		test.NoError(t, err)
		test.MapLen(t, 0, out)
		test.SliceLen(t, 0, client.MGetCalls())
		test.SliceLen(t, 1, cb.CannotProceedCalls())
	})

	T.Run("with redis error", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		impl, client, cb, obs := buildTestImpl(t)

		cb.CannotProceedFunc = func() bool { return false }
		cb.FailedFunc = func() {}

		client.MGetFunc = func(_ context.Context, _ ...string) *redis.SliceCmd {
			cmd := redis.NewSliceCmd(ctx)
			cmd.SetErr(errors.New("redis error"))
			return cmd
		}

		out, err := impl.GetMany(ctx, []string{"a"})
		test.Error(t, err)
		test.Nil(t, out)
		test.SliceLen(t, 1, cb.FailedCalls())

		op := obs.ObservedOperationWithData(t, map[string]any{})
		must.SliceLen(t, 1, op.Errors)
	})

	T.Run("with decode error", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		impl, client, cb, _ := buildTestImpl(t)

		cb.CannotProceedFunc = func() bool { return false }

		client.MGetFunc = func(_ context.Context, _ ...string) *redis.SliceCmd {
			cmd := redis.NewSliceCmd(ctx)
			cmd.SetVal([]any{"not valid gob data"})
			return cmd
		}

		out, err := impl.GetMany(ctx, []string{"a"})
		test.Error(t, err)
		test.Nil(t, out)
	})

	T.Run("cluster mode issues one MGET per slot", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		impl, client, cb, _ := buildTestImpl(t)
		impl.isCluster = true

		cb.CannotProceedFunc = func() bool { return false }
		cb.SucceededFunc = func() {}

		// Distinct hashtags spread the keys across more than one slot.
		keys := []string{"{alpha}1", "{beta}2", "{alpha}3"}
		expectedGroups := len(groupBySlot(keys))
		must.Greater(t, 1, expectedGroups)

		client.MGetFunc = func(_ context.Context, mgetKeys ...string) *redis.SliceCmd {
			cmd := redis.NewSliceCmd(ctx)
			vals := make([]any, len(mgetKeys))
			cmd.SetVal(vals)
			return cmd
		}

		_, err := impl.GetMany(ctx, keys)
		test.NoError(t, err)
		test.SliceLen(t, expectedGroups, client.MGetCalls())
	})
}

func Test_redisCacheImpl_SetMany_Unit(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		impl, client, cb, _ := buildTestImpl(t)

		cb.CannotProceedFunc = func() bool { return false }
		cb.SucceededFunc = func() {}

		client.EvalFunc = func(_ context.Context, script string, keys []string, args ...any) *redis.Cmd {
			test.EqOp(t, batchSetScript, script)
			// ARGV[1] is the TTL in milliseconds; buildTestImpl uses a minute.
			ttl, ok := args[0].(int64)
			test.True(t, ok)
			test.EqOp(t, time.Minute.Milliseconds(), ttl)
			// One TTL arg plus one encoded value per key.
			test.SliceLen(t, len(keys)+1, args)
			cmd := redis.NewCmd(ctx)
			cmd.SetVal(int64(len(keys)))
			return cmd
		}

		err := impl.SetMany(ctx, map[string]*example{
			"a": {Name: "a"},
			"b": {Name: "b"},
		})
		test.NoError(t, err)
		test.SliceLen(t, 1, client.EvalCalls())
		test.SliceLen(t, 1, cb.SucceededCalls())
	})

	T.Run("empty items short-circuits", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		impl, client, _, _ := buildTestImpl(t)

		test.NoError(t, impl.SetMany(ctx, nil))
		test.SliceLen(t, 0, client.EvalCalls())
	})

	T.Run("when circuit breaker cannot proceed", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		impl, client, cb, _ := buildTestImpl(t)

		cb.CannotProceedFunc = func() bool { return true }

		test.NoError(t, impl.SetMany(ctx, map[string]*example{"a": {Name: "a"}}))
		test.SliceLen(t, 0, client.EvalCalls())
		test.SliceLen(t, 1, cb.CannotProceedCalls())
	})

	T.Run("with redis error", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		impl, client, cb, _ := buildTestImpl(t)

		cb.CannotProceedFunc = func() bool { return false }
		cb.FailedFunc = func() {}

		client.EvalFunc = func(_ context.Context, _ string, _ []string, _ ...any) *redis.Cmd {
			cmd := redis.NewCmd(ctx)
			cmd.SetErr(errors.New("redis error"))
			return cmd
		}

		err := impl.SetMany(ctx, map[string]*example{"a": {Name: "a"}})
		test.Error(t, err)
		test.SliceLen(t, 1, cb.FailedCalls())
	})

	T.Run("cluster mode issues one EVAL per slot", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		impl, client, cb, _ := buildTestImpl(t)
		impl.isCluster = true

		cb.CannotProceedFunc = func() bool { return false }
		cb.SucceededFunc = func() {}

		items := map[string]*example{
			"{alpha}1": {Name: "1"},
			"{beta}2":  {Name: "2"},
			"{alpha}3": {Name: "3"},
		}
		keys := make([]string, 0, len(items))
		for k := range items {
			keys = append(keys, k)
		}
		expectedGroups := len(groupBySlot(keys))
		must.Greater(t, 1, expectedGroups)

		client.EvalFunc = func(_ context.Context, _ string, keys []string, _ ...any) *redis.Cmd {
			cmd := redis.NewCmd(ctx)
			cmd.SetVal(int64(len(keys)))
			return cmd
		}

		test.NoError(t, impl.SetMany(ctx, items))
		test.SliceLen(t, expectedGroups, client.EvalCalls())
	})
}

func Test_redisCacheImpl_SetMany_GetMany(T *testing.T) {
	T.Parallel()

	T.Run("round trip against a real redis", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()

		cfg := buildContainerBackedRedisConfig(t)
		c, err := NewRedisCache[example](cfg, time.Minute, nil, nil, nil, nil)
		must.NoError(t, err)

		items := map[string]*example{
			"k1": {Name: "one"},
			"k2": {Name: "two"},
		}
		test.NoError(t, c.SetMany(ctx, items))

		out, getErr := c.GetMany(ctx, []string{"k1", "k2", "k3"})
		test.NoError(t, getErr)
		test.MapLen(t, 2, out)
		test.Eq(t, items["k1"], out["k1"])
		test.Eq(t, items["k2"], out["k2"])
	})
}

func Test_buildRedisClient(T *testing.T) {
	T.Parallel()

	T.Run("with single address", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{
			QueueAddresses: []string{"localhost:6379"},
			Username:       "user",
			Password:       "pass",
		}

		c := buildRedisClient(cfg)
		test.NotNil(t, c)
	})

	T.Run("with multiple addresses", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{
			QueueAddresses: []string{"localhost:6379", "localhost:6380"},
			Username:       "user",
			Password:       "pass",
		}

		c := buildRedisClient(cfg)
		test.NotNil(t, c)
	})

	T.Run("with no addresses", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{
			QueueAddresses: []string{},
		}

		c := buildRedisClient(cfg)
		test.Nil(t, c)
	})
}

func Test_redisCacheImpl_Set_ExpiryOptions_Unit(T *testing.T) {
	T.Parallel()

	T.Run("WithExpiry overrides the configured default", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		impl, client, cb, _ := buildTestImpl(t)

		cb.CannotProceedFunc = func() bool { return false }
		cb.SucceededFunc = func() {}

		client.SetFunc = func(_ context.Context, _ string, _ any, expiration time.Duration) *redis.StatusCmd {
			test.EqOp(t, 5*time.Minute, expiration)
			cmd := redis.NewStatusCmd(ctx)
			cmd.SetVal("OK")
			return cmd
		}

		err := impl.Set(ctx, exampleKey, &example{Name: t.Name()}, cache.WithExpiry(5*time.Minute))
		test.NoError(t, err)
		test.SliceLen(t, 1, client.SetCalls())
	})

	T.Run("NoExpiry stores without expiration", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		impl, client, cb, _ := buildTestImpl(t)

		cb.CannotProceedFunc = func() bool { return false }
		cb.SucceededFunc = func() {}

		client.SetFunc = func(_ context.Context, _ string, _ any, expiration time.Duration) *redis.StatusCmd {
			test.EqOp(t, time.Duration(0), expiration)
			cmd := redis.NewStatusCmd(ctx)
			cmd.SetVal("OK")
			return cmd
		}

		err := impl.Set(ctx, exampleKey, &example{Name: t.Name()}, cache.WithExpiry(cache.NoExpiry))
		test.NoError(t, err)
		test.SliceLen(t, 1, client.SetCalls())
	})

	T.Run("SetMany forwards the resolved expiry to the batch script", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		impl, client, cb, _ := buildTestImpl(t)

		cb.CannotProceedFunc = func() bool { return false }
		cb.SucceededFunc = func() {}

		client.EvalFunc = func(_ context.Context, _ string, _ []string, args ...any) *redis.Cmd {
			must.SliceNotEmpty(t, args)
			gotExpiry, ok := args[0].(int64)
			must.True(t, ok)
			test.EqOp(t, (5 * time.Minute).Milliseconds(), gotExpiry)
			cmd := redis.NewCmd(ctx)
			cmd.SetVal(int64(1))
			return cmd
		}

		err := impl.SetMany(ctx, map[string]*example{exampleKey: {Name: t.Name()}}, cache.WithExpiry(5*time.Minute))
		test.NoError(t, err)
		test.SliceLen(t, 1, client.EvalCalls())
	})
}

// nameCodec stores only the Name field as raw bytes — a stand-in for a
// consumer-supplied fixed-format codec.
type nameCodec struct{}

func (nameCodec) Encode(value *example) ([]byte, error) {
	return []byte(value.Name), nil
}

func (nameCodec) Decode(data []byte) (*example, error) {
	return &example{Name: string(data)}, nil
}

// nilCodec decodes every payload to a nil value without erroring, standing in
// for a codec whose wire form has a legitimate "absent" encoding. The cache
// treats that as a miss rather than handing back a nil pointer.
type nilCodec struct{}

func (nilCodec) Encode(*example) ([]byte, error) { return []byte("whatever"), nil }

func (nilCodec) Decode([]byte) (*example, error) { return nil, nil }

// brokenCodec fails to encode, standing in for a value the configured codec
// cannot represent.
type brokenCodec struct{}

var errCodecBroken = errors.New("codec cannot encode this")

func (brokenCodec) Encode(*example) ([]byte, error) { return nil, errCodecBroken }

func (brokenCodec) Decode([]byte) (*example, error) { return nil, errCodecBroken }

func Test_redisCacheImpl_OpenCircuit_Unit(T *testing.T) {
	T.Parallel()

	// An open breaker short-circuits every write path into a silent success:
	// a cache that cannot be reached must not fail the request behind it.
	openBreaker := func(t *testing.T) (*redisCacheImpl[example], *redisClientMock) {
		t.Helper()

		impl, client, cb, _ := buildTestImpl(t)
		cb.CannotProceedFunc = func() bool { return true }

		return impl, client
	}

	T.Run("Set is a no-op", func(t *testing.T) {
		t.Parallel()

		impl, client := openBreaker(t)

		must.NoError(t, impl.Set(t.Context(), exampleKey, &example{Name: "spot"}))
		test.SliceLen(t, 0, client.SetCalls())
	})

	T.Run("DeleteMany is a no-op", func(t *testing.T) {
		t.Parallel()

		impl, client := openBreaker(t)

		must.NoError(t, impl.DeleteMany(t.Context(), []string{"a", "b"}))
		test.SliceLen(t, 0, client.DelCalls())
	})

	T.Run("DeleteByPrefix is a no-op", func(t *testing.T) {
		t.Parallel()

		impl, client := openBreaker(t)
		impl.namespace = "ns:"

		must.NoError(t, impl.DeleteByPrefix(t.Context(), "p:"))
		test.SliceLen(t, 0, client.ScanCalls())
	})

	T.Run("SetMany is a no-op", func(t *testing.T) {
		t.Parallel()

		impl, client := openBreaker(t)

		must.NoError(t, impl.SetMany(t.Context(), map[string]*example{"a": {Name: "spot"}}))
		test.SliceLen(t, 0, client.EvalCalls())
	})
}

func Test_redisCacheImpl_CodecFailures_Unit(T *testing.T) {
	T.Parallel()

	T.Run("Get treats a nil decode as a miss", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		impl, client, cb, _ := buildTestImpl(t)
		WithCodec[example](nilCodec{})(impl)

		cb.CannotProceedFunc = func() bool { return false }
		cb.SucceededFunc = func() {}

		client.GetFunc = func(context.Context, string) *redis.StringCmd {
			cmd := redis.NewStringCmd(ctx)
			cmd.SetVal("whatever")
			return cmd
		}

		got, err := impl.Get(ctx, exampleKey)
		test.ErrorIs(t, err, cache.ErrNotFound)
		test.Nil(t, got)
	})

	T.Run("GetMany treats a nil decode as a miss", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		impl, client, cb, _ := buildTestImpl(t)
		WithCodec[example](nilCodec{})(impl)

		cb.CannotProceedFunc = func() bool { return false }
		cb.SucceededFunc = func() {}

		client.MGetFunc = func(context.Context, ...string) *redis.SliceCmd {
			cmd := redis.NewSliceCmd(ctx)
			cmd.SetVal([]any{"whatever"})
			return cmd
		}

		out, err := impl.GetMany(ctx, []string{exampleKey})
		must.NoError(t, err)
		test.MapLen(t, 0, out)
	})

	T.Run("Set surfaces an encoding failure without touching the client", func(t *testing.T) {
		t.Parallel()

		impl, client, cb, _ := buildTestImpl(t)
		WithCodec[example](brokenCodec{})(impl)

		cb.CannotProceedFunc = func() bool { return false }

		test.Error(t, impl.Set(t.Context(), exampleKey, &example{Name: "spot"}))
		test.SliceLen(t, 0, client.SetCalls())
	})

	T.Run("SetMany fails the whole batch before any write", func(t *testing.T) {
		t.Parallel()

		impl, client, cb, _ := buildTestImpl(t)
		WithCodec[example](brokenCodec{})(impl)

		cb.CannotProceedFunc = func() bool { return false }

		test.Error(t, impl.SetMany(t.Context(), map[string]*example{"a": {Name: "spot"}}))
		test.SliceLen(t, 0, client.EvalCalls())
	})
}

func Test_redisCacheImpl_CustomCodec_Unit(T *testing.T) {
	T.Parallel()

	T.Run("Set stores the codec's bytes and Get decodes them", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		impl, client, cb, _ := buildTestImpl(t)
		WithCodec[example](nameCodec{})(impl)

		cb.CannotProceedFunc = func() bool { return false }
		cb.SucceededFunc = func() {}

		client.SetFunc = func(_ context.Context, _ string, value any, _ time.Duration) *redis.StatusCmd {
			s, isString := value.(string)
			must.True(t, isString)
			test.EqOp(t, "beeline", s)
			cmd := redis.NewStatusCmd(ctx)
			cmd.SetVal("OK")
			return cmd
		}
		client.GetFunc = func(_ context.Context, _ string) *redis.StringCmd {
			cmd := redis.NewStringCmd(ctx)
			cmd.SetVal("beeline")
			return cmd
		}

		must.NoError(t, impl.Set(ctx, exampleKey, &example{Name: "beeline"}))

		got, err := impl.Get(ctx, exampleKey)
		must.NoError(t, err)
		test.EqOp(t, "beeline", got.Name)
	})

	T.Run("nil codec option is ignored, keeping the gob default", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		impl, client, cb, _ := buildTestImpl(t)
		WithCodec[example](nil)(impl)

		cb.CannotProceedFunc = func() bool { return false }
		cb.SucceededFunc = func() {}

		expected := gobEncodeExample(t, &example{Name: "beeline"})
		client.SetFunc = func(_ context.Context, _ string, value any, _ time.Duration) *redis.StatusCmd {
			s, isString := value.(string)
			must.True(t, isString)
			test.EqOp(t, expected, s)
			cmd := redis.NewStatusCmd(ctx)
			cmd.SetVal("OK")
			return cmd
		}

		must.NoError(t, impl.Set(ctx, exampleKey, &example{Name: "beeline"}))
	})
}

func Test_redisCacheImpl_Namespace_Unit(T *testing.T) {
	T.Parallel()

	T.Run("keys are namespaced on the wire and bare in results", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		impl, client, cb, _ := buildTestImpl(t)
		impl.namespace = "beeline:"

		cb.CannotProceedFunc = func() bool { return false }
		cb.SucceededFunc = func() {}

		client.SetFunc = func(_ context.Context, key string, _ any, _ time.Duration) *redis.StatusCmd {
			test.EqOp(t, "beeline:"+exampleKey, key)
			cmd := redis.NewStatusCmd(ctx)
			cmd.SetVal("OK")
			return cmd
		}
		client.MGetFunc = func(_ context.Context, keys ...string) *redis.SliceCmd {
			must.SliceLen(t, 1, keys)
			test.EqOp(t, "beeline:"+exampleKey, keys[0])
			cmd := redis.NewSliceCmd(ctx)
			cmd.SetVal([]any{gobEncodeExample(t, &example{Name: "spot"})})
			return cmd
		}

		must.NoError(t, impl.Set(ctx, exampleKey, &example{Name: "spot"}))

		out, err := impl.GetMany(ctx, []string{exampleKey})
		must.NoError(t, err)
		must.MapLen(t, 1, out)
		// The result map is keyed by the caller's bare key, not the stored one.
		must.NotNil(t, out[exampleKey])
		test.EqOp(t, "spot", out[exampleKey].Name)
	})
}

func Test_redisCacheImpl_Deletion_Unit(T *testing.T) {
	T.Parallel()

	T.Run("DeleteMany issues one DEL with namespaced keys", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		impl, client, cb, _ := buildTestImpl(t)
		impl.namespace = "ns:"

		cb.CannotProceedFunc = func() bool { return false }
		cb.SucceededFunc = func() {}

		client.DelFunc = func(_ context.Context, keys ...string) *redis.IntCmd {
			test.Eq(t, []string{"ns:a", "ns:b"}, keys)
			cmd := redis.NewIntCmd(ctx)
			cmd.SetVal(int64(len(keys)))
			return cmd
		}

		must.NoError(t, impl.DeleteMany(ctx, []string{"a", "b"}))
		test.SliceLen(t, 1, client.DelCalls())
	})

	T.Run("Flush without a namespace is refused", func(t *testing.T) {
		t.Parallel()

		impl, _, _, _ := buildTestImpl(t)

		test.ErrorIs(t, impl.Flush(t.Context()), cache.ErrNamespaceRequired)
	})

	T.Run("DeleteByPrefix with no namespace and empty prefix is refused", func(t *testing.T) {
		t.Parallel()

		impl, _, _, _ := buildTestImpl(t)

		test.ErrorIs(t, impl.DeleteByPrefix(t.Context(), ""), cache.ErrNamespaceRequired)
	})

	T.Run("Flush scans the namespace pattern and deletes what it finds", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		impl, client, cb, _ := buildTestImpl(t)
		impl.namespace = "ns:"

		cb.CannotProceedFunc = func() bool { return false }
		cb.SucceededFunc = func() {}

		pages := [][]string{{"ns:a", "ns:b"}, {"ns:c"}}
		cursors := []uint64{7, 0}
		call := 0
		client.ScanFunc = func(_ context.Context, cursor uint64, match string, count int64) *redis.ScanCmd {
			test.EqOp(t, "ns:*", match)
			cmd := redis.NewScanCmd(ctx, nil)
			cmd.SetVal(pages[call], cursors[call])
			call++
			return cmd
		}

		var deleted []string
		client.DelFunc = func(_ context.Context, keys ...string) *redis.IntCmd {
			deleted = append(deleted, keys...)
			cmd := redis.NewIntCmd(ctx)
			cmd.SetVal(int64(len(keys)))
			return cmd
		}

		must.NoError(t, impl.Flush(ctx))
		test.Eq(t, []string{"ns:a", "ns:b", "ns:c"}, deleted)
		test.SliceLen(t, 2, client.ScanCalls())
	})

	T.Run("DeleteByPrefix escapes glob metacharacters in the prefix", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		impl, client, cb, _ := buildTestImpl(t)
		impl.namespace = "ns:"

		cb.CannotProceedFunc = func() bool { return false }
		cb.SucceededFunc = func() {}

		client.ScanFunc = func(_ context.Context, _ uint64, match string, _ int64) *redis.ScanCmd {
			test.EqOp(t, `ns:area\[1\]:*`, match)
			cmd := redis.NewScanCmd(ctx, nil)
			cmd.SetVal([]string{}, 0)
			return cmd
		}

		must.NoError(t, impl.DeleteByPrefix(ctx, "area[1]:"))
	})
}

func Test_redisCacheImpl_Deletion_Failures_Unit(T *testing.T) {
	T.Parallel()

	T.Run("DeleteMany with no keys never reaches the client", func(t *testing.T) {
		t.Parallel()

		impl, client, _, _ := buildTestImpl(t)

		must.NoError(t, impl.DeleteMany(t.Context(), nil))
		test.SliceLen(t, 0, client.DelCalls())
	})

	T.Run("DeleteMany reports a failed DEL and trips the breaker", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		impl, client, cb, _ := buildTestImpl(t)

		var failed int
		cb.CannotProceedFunc = func() bool { return false }
		cb.FailedFunc = func() { failed++ }

		client.DelFunc = func(context.Context, ...string) *redis.IntCmd {
			cmd := redis.NewIntCmd(ctx)
			cmd.SetErr(errors.New("connection reset"))
			return cmd
		}

		test.Error(t, impl.DeleteMany(ctx, []string{"a"}))
		test.EqOp(t, 1, failed)
	})

	T.Run("DeleteByPrefix reports a failed SCAN and trips the breaker", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		impl, client, cb, _ := buildTestImpl(t)
		impl.namespace = "ns:"

		var failed int
		cb.CannotProceedFunc = func() bool { return false }
		cb.FailedFunc = func() { failed++ }

		client.ScanFunc = func(context.Context, uint64, string, int64) *redis.ScanCmd {
			cmd := redis.NewScanCmd(ctx, nil)
			cmd.SetErr(errors.New("connection reset"))
			return cmd
		}

		test.Error(t, impl.DeleteByPrefix(ctx, "p:"))
		test.EqOp(t, 1, failed)
	})

	T.Run("DeleteByPrefix reports a failed DEL mid-scan", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		impl, client, cb, _ := buildTestImpl(t)
		impl.namespace = "ns:"

		cb.CannotProceedFunc = func() bool { return false }
		cb.FailedFunc = func() {}

		client.ScanFunc = func(context.Context, uint64, string, int64) *redis.ScanCmd {
			cmd := redis.NewScanCmd(ctx, nil)
			// A non-zero cursor: the scan would continue if the DEL succeeded.
			cmd.SetVal([]string{"ns:a"}, 9)
			return cmd
		}
		client.DelFunc = func(context.Context, ...string) *redis.IntCmd {
			cmd := redis.NewIntCmd(ctx)
			cmd.SetErr(errors.New("connection reset"))
			return cmd
		}

		test.Error(t, impl.DeleteByPrefix(ctx, "p:"))
		// The failure stopped the cursor rather than looping forever.
		test.SliceLen(t, 1, client.ScanCalls())
	})

	T.Run("a cluster client scans every master", func(t *testing.T) {
		t.Parallel()

		// A real ClusterClient is the only way into the ForEachMaster branch —
		// the type is asserted, not interface-dispatched. Pointing it at a
		// closed port makes the fan-out fail fast, which is enough to prove the
		// branch was taken.
		impl, _, cb, _ := buildTestImpl(t)
		impl.namespace = "ns:"
		impl.isCluster = true
		impl.client = redis.NewClusterClient(&redis.ClusterOptions{
			Addrs:        []string{"127.0.0.1:1"},
			DialTimeout:  100 * time.Millisecond,
			ReadTimeout:  100 * time.Millisecond,
			WriteTimeout: 100 * time.Millisecond,
			MaxRedirects: 1,
		})

		cb.CannotProceedFunc = func() bool { return false }
		cb.FailedFunc = func() {}

		test.Error(t, impl.DeleteByPrefix(t.Context(), "p:"))
	})
}

func TestWithScanPageSize(T *testing.T) {
	T.Parallel()

	// scanCount drives a one-page prefix deletion and reports the COUNT the
	// cache actually handed to SCAN, so these assert the option reaches the
	// wire rather than just landing in a struct field.
	scanCount := func(t *testing.T, impl *redisCacheImpl[example], client *redisClientMock, cb *mockcircuitbreaking.CircuitBreakerMock) int64 {
		t.Helper()

		ctx := t.Context()
		impl.namespace = "ns:"
		cb.CannotProceedFunc = func() bool { return false }
		cb.SucceededFunc = func() {}

		var got int64
		client.ScanFunc = func(_ context.Context, _ uint64, _ string, count int64) *redis.ScanCmd {
			got = count
			cmd := redis.NewScanCmd(ctx, nil)
			cmd.SetVal([]string{}, 0)
			return cmd
		}

		must.NoError(t, impl.DeleteByPrefix(ctx, "p:"))

		return got
	}

	T.Run("the configured size reaches SCAN", func(t *testing.T) {
		t.Parallel()

		impl, client, cb, _ := buildTestImpl(t)
		WithScanPageSize[example](25)(impl)

		test.EqOp(t, int64(25), scanCount(t, impl, client, cb))
	})

	T.Run("the default reaches SCAN when the option is not supplied", func(t *testing.T) {
		t.Parallel()

		impl, client, cb, _ := buildTestImpl(t)

		test.EqOp(t, int64(defaultScanPageSize), scanCount(t, impl, client, cb))
	})

	T.Run("a non-positive size is ignored", func(t *testing.T) {
		t.Parallel()

		for _, size := range []int64{0, -1} {
			impl, client, cb, _ := buildTestImpl(t)
			WithScanPageSize[example](size)(impl)

			test.EqOp(t, int64(defaultScanPageSize), scanCount(t, impl, client, cb))
		}
	})

	T.Run("NewRedisCache applies the option", func(t *testing.T) {
		t.Parallel()

		c, err := NewRedisCache[example](
			&Config{QueueAddresses: []string{"localhost:6379"}},
			time.Minute, nil, nil, nil, nil,
			WithScanPageSize[example](64),
		)
		must.NoError(t, err)

		impl, ok := c.(*redisCacheImpl[example])
		must.True(t, ok)
		test.EqOp(t, int64(64), impl.scanPageSize)
	})
}
