package jobscfg

import (
	"context"
	"testing"
	"time"

	distributedlockcfg "github.com/primandproper/platform-go/v8/distributedlock/config"
	pglock "github.com/primandproper/platform-go/v8/distributedlock/postgres"
	"github.com/primandproper/platform-go/v8/jobs"
	msgconfig "github.com/primandproper/platform-go/v8/messagequeue/config"
	"github.com/primandproper/platform-go/v8/messagequeue/pubsub"
	loggingnoop "github.com/primandproper/platform-go/v8/observability/logging/noop"
	metricsnoop "github.com/primandproper/platform-go/v8/observability/metrics/noop"
	tracingnoop "github.com/primandproper/platform-go/v8/observability/tracing/noop"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// poolConfig is a valid in-process configuration: a topic on the noop consumer.
func poolConfig() *PoolConfig {
	return &PoolConfig{
		Pool: jobs.PoolConfig{Topic: "background_work"},
	}
}

// schedulerConfig is a valid in-process configuration: memory locks.
func schedulerConfig() *SchedulerConfig {
	return &SchedulerConfig{
		Lock: distributedlockcfg.Config{Provider: distributedlockcfg.MemoryProvider},
	}
}

func TestPoolConfig_EnsureDefaults(T *testing.T) {
	T.Parallel()

	T.Run("fills in zero fields", func(t *testing.T) {
		t.Parallel()

		cfg := poolConfig()
		cfg.EnsureDefaults()

		test.EqOp(t, jobs.DefaultConcurrency, cfg.Pool.Concurrency)
	})
}

func TestPoolConfig_ValidateWithContext(T *testing.T) {
	T.Parallel()

	T.Run("accepts a valid config", func(t *testing.T) {
		t.Parallel()

		cfg := poolConfig()
		cfg.EnsureDefaults()

		test.NoError(t, cfg.ValidateWithContext(t.Context()))
	})

	// The nested config is reached through a validation.By closure, because
	// ozzo would otherwise dereference the struct and skip it.
	T.Run("rejects a missing topic", func(t *testing.T) {
		t.Parallel()

		cfg := &PoolConfig{}
		cfg.EnsureDefaults()

		test.Error(t, cfg.ValidateWithContext(t.Context()))
	})
}

func TestNewPool(T *testing.T) {
	T.Parallel()

	handler := func(context.Context, []byte) error { return nil }

	T.Run("builds a pool on the noop consumer by default", func(t *testing.T) {
		t.Parallel()

		p, err := NewPool(t.Context(), poolConfig(), loggingnoop.NewLogger(), nil, nil, handler)
		must.NoError(t, err)
		must.NotNil(t, p)
	})

	T.Run("rejects a nil config", func(t *testing.T) {
		t.Parallel()

		_, err := NewPool(t.Context(), nil, loggingnoop.NewLogger(), nil, nil, handler)
		test.Error(t, err)
	})

	T.Run("rejects a nil handler", func(t *testing.T) {
		t.Parallel()

		_, err := NewPool(t.Context(), poolConfig(), loggingnoop.NewLogger(), nil, nil, nil)
		test.Error(t, err)
	})

	T.Run("rejects a config that fails validation", func(t *testing.T) {
		t.Parallel()

		_, err := NewPool(t.Context(), &PoolConfig{}, loggingnoop.NewLogger(), nil, nil, handler)
		test.Error(t, err)
	})

	T.Run("surfaces a consumer provider that will not build", func(t *testing.T) {
		t.Parallel()

		// PubSub with no project ID fails client construction, which is the
		// cheapest way to make the provider step fail without a network.
		cfg := poolConfig()
		cfg.Queue = msgconfig.Config{
			Consumer: msgconfig.MessageQueueConfig{
				Provider: msgconfig.ProviderPubSub,
				PubSub:   pubsub.Config{},
			},
		}

		p, err := NewPool(t.Context(), cfg, loggingnoop.NewLogger(), nil, nil, handler)
		test.Nil(t, p)
		test.Error(t, err)
	})

	T.Run("derives options from every observability argument", func(t *testing.T) {
		t.Parallel()

		p, err := NewPool(
			t.Context(),
			poolConfig(),
			loggingnoop.NewLogger(),
			tracingnoop.NewTracerProvider(),
			metricsnoop.NewMetricsProvider(),
			handler,
		)
		must.NoError(t, err)
		must.NotNil(t, p)
	})
}

func TestSchedulerConfig_EnsureDefaults(T *testing.T) {
	T.Parallel()

	T.Run("fills in zero fields", func(t *testing.T) {
		t.Parallel()

		cfg := schedulerConfig()
		cfg.EnsureDefaults()

		test.EqOp(t, jobs.DefaultLockKeyPrefix, cfg.Scheduler.LockKeyPrefix)
		test.EqOp(t, jobs.DefaultLeaseTTL, cfg.Scheduler.DefaultLeaseTTL)
	})

	T.Run("leaves set fields alone", func(t *testing.T) {
		t.Parallel()

		cfg := schedulerConfig()
		cfg.Scheduler.DefaultLeaseTTL = time.Minute
		cfg.EnsureDefaults()

		test.EqOp(t, time.Minute, cfg.Scheduler.DefaultLeaseTTL)
	})
}

func TestSchedulerConfig_ValidateWithContext(T *testing.T) {
	T.Parallel()

	T.Run("accepts a valid config", func(t *testing.T) {
		t.Parallel()

		cfg := schedulerConfig()
		cfg.EnsureDefaults()

		test.NoError(t, cfg.ValidateWithContext(t.Context()))
	})

	T.Run("rejects an invalid nested lock provider", func(t *testing.T) {
		t.Parallel()

		cfg := schedulerConfig()
		cfg.EnsureDefaults()
		cfg.Lock.Provider = "carrier-pigeon"

		test.Error(t, cfg.ValidateWithContext(t.Context()))
	})
}

func TestNewScheduler(T *testing.T) {
	T.Parallel()

	T.Run("builds a scheduler on memory locks", func(t *testing.T) {
		t.Parallel()

		s, err := NewScheduler(t.Context(), schedulerConfig(), loggingnoop.NewLogger(), nil, nil, nil)
		must.NoError(t, err)
		must.NotNil(t, s)
	})

	T.Run("rejects a nil config", func(t *testing.T) {
		t.Parallel()

		_, err := NewScheduler(t.Context(), nil, loggingnoop.NewLogger(), nil, nil, nil)
		test.Error(t, err)
	})

	T.Run("rejects a config that fails validation", func(t *testing.T) {
		t.Parallel()

		cfg := schedulerConfig()
		cfg.Lock = distributedlockcfg.Config{Provider: "not-a-provider"}

		_, err := NewScheduler(t.Context(), cfg, loggingnoop.NewLogger(), nil, nil, nil)
		test.Error(t, err)
	})

	T.Run("surfaces a locker that will not build", func(t *testing.T) {
		t.Parallel()

		// The config validates — the postgres provider has its section — but the
		// locker itself needs a database client, and db is nil here.
		cfg := schedulerConfig()
		cfg.Lock = distributedlockcfg.Config{
			Provider: distributedlockcfg.PostgresProvider,
			Postgres: &pglock.Config{},
		}

		s, err := NewScheduler(t.Context(), cfg, loggingnoop.NewLogger(), nil, nil, nil)
		test.Nil(t, s)
		test.Error(t, err)
	})

	T.Run("derives options from every observability argument", func(t *testing.T) {
		t.Parallel()

		s, err := NewScheduler(
			t.Context(),
			schedulerConfig(),
			loggingnoop.NewLogger(),
			tracingnoop.NewTracerProvider(),
			metricsnoop.NewMetricsProvider(),
			nil,
		)
		must.NoError(t, err)
		must.NotNil(t, s)
	})
}
