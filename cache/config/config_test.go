package cachecfg

import (
	"errors"
	"testing"

	"github.com/primandproper/platform-go/v9/cache/redis"
	circuitbreakingcfg "github.com/primandproper/platform-go/v9/circuitbreaking/config"
	"github.com/primandproper/platform-go/v9/observability"
	"github.com/primandproper/platform-go/v9/observability/metrics"
	metricsmock "github.com/primandproper/platform-go/v9/observability/metrics/mock"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
	"go.opentelemetry.io/otel/metric"
)

type example struct {
	Name string `json:"name"`
}

func TestConfig_ValidateWithContext(T *testing.T) {
	T.Parallel()

	T.Run("memory provider", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		cfg := &Config{
			Provider: ProviderMemory,
		}

		test.NoError(t, cfg.ValidateWithContext(ctx))
	})

	T.Run("redis provider with config", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{
			Provider: ProviderRedis,
			Redis:    &redis.Config{Addresses: []string{"localhost:6379"}},
		}

		test.NoError(t, cfg.ValidateWithContext(t.Context()))
	})

	T.Run("redis provider missing config", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{Provider: ProviderRedis}
		test.Error(t, cfg.ValidateWithContext(t.Context()))
	})

	T.Run("invalid provider name", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{Provider: "vault"}
		test.Error(t, cfg.ValidateWithContext(t.Context()))
	})
}

func TestNewCache(T *testing.T) {
	T.Parallel()

	T.Run("memory provider", func(t *testing.T) {
		t.Parallel()

		c, err := NewCache[example](
			t.Context(),
			&Config{
				Provider: ProviderMemory,
			},
		)

		must.NoError(t, err)
		test.NotNil(t, c)
	})

	T.Run("redis provider", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{
			Provider: ProviderRedis,
			Redis:    &redis.Config{Addresses: []string{"localhost:6379"}},
		}
		cfg.CircuitBreaker.Name = "cache-breaker"

		c, err := NewCache[example](
			t.Context(),
			cfg,
		)

		must.NoError(t, err)
		test.NotNil(t, c)
	})

	T.Run("redis provider with cluster addresses", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{
			Provider: ProviderRedis,
			Redis:    &redis.Config{Addresses: []string{"localhost:6379", "localhost:6380"}},
		}
		cfg.CircuitBreaker.Name = "cache-breaker-cluster"

		c, err := NewCache[example](
			t.Context(),
			cfg,
		)

		must.NoError(t, err)
		test.NotNil(t, c)
	})

	T.Run("redis provider with circuit breaker error", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{
			Provider: ProviderRedis,
			Redis:    &redis.Config{Addresses: []string{"localhost:6379"}},
			CircuitBreaker: circuitbreakingcfg.Config{
				Name:                   "redis-cache-breaker",
				ErrorRate:              50,
				MinimumSampleThreshold: 10,
			},
		}

		mp := &metricsmock.ProviderMock{
			NewInt64CounterFunc: func(name string, _ ...metric.Int64CounterOption) (metrics.Int64Counter, error) {
				test.EqOp(t, "redis-cache-breaker_circuit_breaker_tripped", name)
				return nil, errors.New("counter init failure")
			},
		}

		c, err := NewCache[example](
			t.Context(),
			cfg,
			WithMetricsProvider(mp),
		)

		must.Error(t, err)
		test.Nil(t, c)
		test.SliceLen(t, 1, mp.NewInt64CounterCalls())
	})

	T.Run("invalid provider", func(t *testing.T) {
		t.Parallel()

		_, err := NewCache[example](t.Context(), &Config{})

		test.Error(t, err)
	})
}

func TestNewCache_observabilityOptions(T *testing.T) {
	T.Parallel()

	memoryConfig := func() *Config { return &Config{Provider: ProviderMemory} }

	// breakerConfig selects the redis provider, whose circuit breaker registers
	// counters on whatever metrics provider it was handed. Failing that
	// registration is the cheapest way to observe which provider actually
	// reached the components being built.
	breakerConfig := func() *Config {
		return &Config{
			Provider: ProviderRedis,
			Redis:    &redis.Config{Addresses: []string{"localhost:6379"}},
			CircuitBreaker: circuitbreakingcfg.Config{
				Name:                   "pillars-cache-breaker",
				ErrorRate:              50,
				MinimumSampleThreshold: 10,
			},
		}
	}

	failingProvider := func() *metricsmock.ProviderMock {
		return &metricsmock.ProviderMock{
			NewInt64CounterFunc: func(string, ...metric.Int64CounterOption) (metrics.Int64Counter, error) {
				return nil, errors.New("counter init failure")
			},
		}
	}

	T.Run("builds with no observability at all", func(t *testing.T) {
		t.Parallel()

		// The reason the options exist: a caller that wants none of the three
		// names none of them, rather than naming three noops.
		c, err := NewCache[example](t.Context(), memoryConfig())

		must.NoError(t, err)
		test.NotNil(t, c)
	})

	T.Run("WithPillars supplies the metrics provider", func(t *testing.T) {
		t.Parallel()

		mp := failingProvider()

		c, err := NewCache[example](t.Context(), breakerConfig(), WithPillars(&observability.Pillars{
			MetricsProvider: mp,
		}))

		must.Error(t, err)
		test.Nil(t, c)
		test.SliceLen(t, 1, mp.NewInt64CounterCalls())
	})

	T.Run("a nil Pillars attaches nothing", func(t *testing.T) {
		t.Parallel()

		c, err := NewCache[example](t.Context(), memoryConfig(), WithPillars(nil))

		must.NoError(t, err)
		test.NotNil(t, c)
	})

	T.Run("a later option overrides what the pillars supplied", func(t *testing.T) {
		t.Parallel()

		mp := failingProvider()

		// Options apply in order, so the later nil wins: the breaker's counters
		// go to the noop instead of this mock, and construction succeeds rather
		// than failing on its error.
		c, err := NewCache[example](t.Context(), breakerConfig(),
			WithPillars(&observability.Pillars{MetricsProvider: mp}),
			WithMetricsProvider(nil),
		)

		must.NoError(t, err)
		test.NotNil(t, c)
		test.SliceEmpty(t, mp.NewInt64CounterCalls())
	})
}
