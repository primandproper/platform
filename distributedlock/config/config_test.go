package distributedlockcfg

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	circuitbreakingcfg "github.com/primandproper/platform/circuitbreaking/config"
	"github.com/primandproper/platform/database"
	"github.com/primandproper/platform/distributedlock"
	pglock "github.com/primandproper/platform/distributedlock/postgres"
	redislock "github.com/primandproper/platform/distributedlock/redis"
	"github.com/primandproper/platform/observability/logging"
	"github.com/primandproper/platform/observability/metrics"
	mockmetrics "github.com/primandproper/platform/observability/metrics/mock"
	"github.com/primandproper/platform/observability/tracing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
	"go.opentelemetry.io/otel/metric"
)

// stubDBClient is a minimal database.Client for constructing a postgres locker
// without requiring a real database connection. The locker constructor stores
// the client but does not use it until a lock is acquired.
type stubDBClient struct{}

func (c *stubDBClient) WriteDB() *sql.DB       { return nil }
func (c *stubDBClient) ReadDB() *sql.DB        { return nil }
func (c *stubDBClient) Close() error           { return nil }
func (c *stubDBClient) CurrentTime() time.Time { return time.Now() }
func (c *stubDBClient) RollbackTransaction(_ context.Context, _ database.SQLQueryExecutorAndTransactionManager) {
}

func TestConfig_ValidateWithContext(T *testing.T) {
	T.Parallel()

	T.Run("redis provider", func(t *testing.T) {
		t.Parallel()
		cfg := &Config{
			Provider: RedisProvider,
			Redis: &redislock.Config{
				Addresses: []string{"localhost:6379"},
				KeyPrefix: "lock:",
			},
		}
		test.NoError(t, cfg.ValidateWithContext(t.Context()))
	})

	T.Run("postgres provider", func(t *testing.T) {
		t.Parallel()
		cfg := &Config{
			Provider: PostgresProvider,
			Postgres: &pglock.Config{},
		}
		test.NoError(t, cfg.ValidateWithContext(t.Context()))
	})

	T.Run("memory provider", func(t *testing.T) {
		t.Parallel()
		cfg := &Config{Provider: MemoryProvider}
		test.NoError(t, cfg.ValidateWithContext(t.Context()))
	})

	T.Run("noop provider", func(t *testing.T) {
		t.Parallel()
		cfg := &Config{Provider: NoopProvider}
		test.NoError(t, cfg.ValidateWithContext(t.Context()))
	})

	T.Run("redis without config", func(t *testing.T) {
		t.Parallel()
		cfg := &Config{Provider: RedisProvider}
		test.Error(t, cfg.ValidateWithContext(t.Context()))
	})

	T.Run("postgres without config", func(t *testing.T) {
		t.Parallel()
		cfg := &Config{Provider: PostgresProvider}
		test.Error(t, cfg.ValidateWithContext(t.Context()))
	})

	T.Run("invalid provider", func(t *testing.T) {
		t.Parallel()
		cfg := &Config{Provider: "made-up"}
		test.Error(t, cfg.ValidateWithContext(t.Context()))
	})

	T.Run("empty provider is valid (noop)", func(t *testing.T) {
		t.Parallel()
		cfg := &Config{}
		test.NoError(t, cfg.ValidateWithContext(t.Context()))
	})
}

func TestProvideLocker(T *testing.T) {
	T.Parallel()

	T.Run("nil config", func(t *testing.T) {
		t.Parallel()
		_, err := ProvideLocker(
			t.Context(),
			nil,
			logging.NewNoopLogger(),
			tracing.NewNoopTracerProvider(),
			metrics.NewNoopMetricsProvider(),
			nil,
		)
		test.ErrorIs(t, err, distributedlock.ErrNilConfig)
	})

	T.Run("memory provider returns a working locker", func(t *testing.T) {
		t.Parallel()
		l, err := ProvideLocker(
			t.Context(),
			&Config{Provider: MemoryProvider},
			logging.NewNoopLogger(),
			tracing.NewNoopTracerProvider(),
			metrics.NewNoopMetricsProvider(),
			nil,
		)
		must.NoError(t, err)
		must.NotNil(t, l)
		lock, err := l.Acquire(t.Context(), "k", time.Second)
		must.NoError(t, err)
		must.NoError(t, lock.Release(t.Context()))
	})

	T.Run("noop provider", func(t *testing.T) {
		t.Parallel()
		l, err := ProvideLocker(
			t.Context(),
			&Config{Provider: NoopProvider},
			logging.NewNoopLogger(),
			tracing.NewNoopTracerProvider(),
			metrics.NewNoopMetricsProvider(),
			nil,
		)
		must.NoError(t, err)
		must.NotNil(t, l)
	})

	T.Run("unknown provider returns noop", func(t *testing.T) {
		t.Parallel()
		l, err := ProvideLocker(
			t.Context(),
			&Config{Provider: "unknown"},
			logging.NewNoopLogger(),
			tracing.NewNoopTracerProvider(),
			metrics.NewNoopMetricsProvider(),
			nil,
		)
		must.NoError(t, err)
		must.NotNil(t, l)
	})

	T.Run("empty provider returns noop", func(t *testing.T) {
		t.Parallel()
		l, err := ProvideLocker(
			t.Context(),
			&Config{},
			logging.NewNoopLogger(),
			tracing.NewNoopTracerProvider(),
			metrics.NewNoopMetricsProvider(),
			nil,
		)
		must.NoError(t, err)
		must.NotNil(t, l)
	})

	T.Run("provider with whitespace returns noop", func(t *testing.T) {
		t.Parallel()
		l, err := ProvideLocker(
			t.Context(),
			&Config{Provider: "   "},
			logging.NewNoopLogger(),
			tracing.NewNoopTracerProvider(),
			metrics.NewNoopMetricsProvider(),
			nil,
		)
		must.NoError(t, err)
		must.NotNil(t, l)
	})

	T.Run("redis provider", func(t *testing.T) {
		t.Parallel()
		l, err := ProvideLocker(
			t.Context(),
			&Config{
				Provider: RedisProvider,
				Redis: &redislock.Config{
					Addresses: []string{"localhost:6379"},
					KeyPrefix: "lock:",
				},
			},
			logging.NewNoopLogger(),
			tracing.NewNoopTracerProvider(),
			metrics.NewNoopMetricsProvider(),
			nil,
		)
		must.NoError(t, err)
		must.NotNil(t, l)
	})

	T.Run("postgres provider", func(t *testing.T) {
		t.Parallel()
		l, err := ProvideLocker(
			t.Context(),
			&Config{
				Provider: PostgresProvider,
				Postgres: &pglock.Config{},
			},
			logging.NewNoopLogger(),
			tracing.NewNoopTracerProvider(),
			metrics.NewNoopMetricsProvider(),
			&stubDBClient{},
		)
		must.NoError(t, err)
		must.NotNil(t, l)
	})

	T.Run("circuit breaker init failure", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{
			CircuitBreaker: circuitbreakingcfg.Config{
				Name:                   "dlock-breaker",
				ErrorRate:              50,
				MinimumSampleThreshold: 10,
			},
		}

		mp := &mockmetrics.ProviderMock{
			NewInt64CounterFunc: func(counterName string, _ ...metric.Int64CounterOption) (metrics.Int64Counter, error) {
				test.EqOp(t, "dlock-breaker_circuit_breaker_tripped", counterName)
				return &mockmetrics.Int64CounterMock{}, fmt.Errorf("counter init failure")
			},
		}

		l, err := ProvideLocker(
			t.Context(),
			cfg,
			logging.NewNoopLogger(),
			tracing.NewNoopTracerProvider(),
			mp,
			nil,
		)
		must.Error(t, err)
		test.Nil(t, l)
		test.StrContains(t, err.Error(), "distributedlock circuit breaker")

		test.SliceLen(t, 1, mp.NewInt64CounterCalls())
	})
}
