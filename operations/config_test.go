package operations

import (
	"testing"
	"time"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestConfig_EnsureDefaults(T *testing.T) {
	T.Parallel()

	T.Run("an empty config becomes a usable one", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{}
		cfg.EnsureDefaults()

		test.EqOp(t, DefaultQueueName, cfg.QueueName)
		test.EqOp(t, DefaultRetention, cfg.Retention)
		test.EqOp(t, DefaultRecoverAfter, cfg.RecoverAfter)
		test.EqOp(t, DefaultReapBatchSize, cfg.ReapBatchSize)
		test.EqOp(t, DefaultRecoverBatchSize, cfg.RecoverBatchSize)

		// Defaults are applied before validation, so an unset field with a
		// documented default is not a validation failure.
		test.NoError(t, cfg.ValidateWithContext(t.Context()))
	})

	T.Run("set values are left alone", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{
			QueueName:        "exports",
			Retention:        time.Hour,
			RecoverAfter:     time.Minute * 5,
			ReapBatchSize:    7,
			RecoverBatchSize: 9,
		}
		cfg.EnsureDefaults()

		test.EqOp(t, "exports", cfg.QueueName)
		test.EqOp(t, time.Hour, cfg.Retention)
		test.EqOp(t, 5*time.Minute, cfg.RecoverAfter)
		test.EqOp(t, 7, cfg.ReapBatchSize)
		test.EqOp(t, 9, cfg.RecoverBatchSize)
	})
}

func TestConfig_Validate(T *testing.T) {
	T.Parallel()

	for name, mutate := range map[string]func(*Config){
		"no queue name":            func(c *Config) { c.QueueName = "" },
		"retention under a minute": func(c *Config) { c.Retention = time.Second },
		"recover under a second":   func(c *Config) { c.RecoverAfter = time.Millisecond },
		"no reap batch":            func(c *Config) { c.ReapBatchSize = -1 },
		"no recover batch":         func(c *Config) { c.RecoverBatchSize = -1 },
	} {
		T.Run(name, func(t *testing.T) {
			t.Parallel()

			cfg := &Config{}
			cfg.EnsureDefaults()
			mutate(cfg)

			test.Error(t, cfg.ValidateWithContext(t.Context()))
		})
	}
}

func TestWorkerConfig(T *testing.T) {
	T.Parallel()

	T.Run("an empty config becomes a usable one", func(t *testing.T) {
		t.Parallel()

		cfg := &WorkerConfig{}
		cfg.EnsureDefaults()

		test.EqOp(t, DefaultWorkerPoll, cfg.Poll)
		test.EqOp(t, DefaultWorkerLease, cfg.Lease)
		test.EqOp(t, DefaultWorkerBatch, cfg.Batch)
		test.EqOp(t, DefaultWorkerConcurrency, cfg.Concurrency)
		test.EqOp(t, DefaultWorkerMaxAttempts, cfg.MaxAttempts)
		test.EqOp(t, DefaultProgressInterval, cfg.ProgressInterval)

		test.NoError(t, cfg.ValidateWithContext(t.Context()))
	})

	// The one cross-field rule, and the one worth having: a flush is what
	// extends the lease, so an interval that does not comfortably fit inside the
	// lease guarantees every operation reporting progress is reclaimed between
	// flushes and run twice.
	T.Run("the progress interval must fit inside the lease", func(t *testing.T) {
		t.Parallel()

		cfg := &WorkerConfig{Lease: 10 * time.Second, ProgressInterval: 9 * time.Second}
		cfg.EnsureDefaults()

		must.Error(t, cfg.ValidateWithContext(t.Context()))

		cfg.ProgressInterval = 5 * time.Second
		test.NoError(t, cfg.ValidateWithContext(t.Context()))
	})

	// Unlimited attempts is exactly the case where an operation never reaches a
	// terminal state, which is the promise the package makes. Zero and negative
	// both default to a real ceiling rather than meaning "no ceiling".
	T.Run("attempts cannot be unlimited", func(t *testing.T) {
		t.Parallel()

		for _, given := range []int{0, -1} {
			cfg := &WorkerConfig{MaxAttempts: given}
			cfg.EnsureDefaults()

			test.EqOp(t, DefaultWorkerMaxAttempts, cfg.MaxAttempts)
			test.NoError(t, cfg.ValidateWithContext(t.Context()))
		}
	})
}

func TestWatcherConfig(T *testing.T) {
	T.Parallel()

	T.Run("an empty config becomes a usable one", func(t *testing.T) {
		t.Parallel()

		cfg := &WatcherConfig{}
		cfg.EnsureDefaults()

		test.EqOp(t, DefaultWatcherPoll, cfg.Poll)
		test.EqOp(t, DefaultWatcherMinReadInterval, cfg.MinReadInterval)
		test.EqOp(t, DefaultWatcherMaxSubscriptions, cfg.MaxSubscriptions)

		test.NoError(t, cfg.ValidateWithContext(t.Context()))
	})

	T.Run("rejects an unusable poll", func(t *testing.T) {
		t.Parallel()

		cfg := &WatcherConfig{}
		cfg.EnsureDefaults()
		cfg.Poll = time.Millisecond

		test.Error(t, cfg.ValidateWithContext(t.Context()))
	})
}
