package workqueue

import (
	"math"
	"testing"
	"time"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func validConfig() *Config {
	return &Config{Name: "jobs"}
}

func TestConfig_EnsureDefaults(T *testing.T) {
	T.Parallel()

	T.Run("fills the unset knobs", func(t *testing.T) {
		t.Parallel()

		cfg := validConfig()
		cfg.EnsureDefaults()

		test.EqOp(t, DefaultRetention, cfg.Retention)
		test.EqOp(t, DefaultMaxClaimBatch, cfg.MaxClaimBatch)
		test.EqOp(t, DefaultReapBatchSize, cfg.ReapBatchSize)
		test.EqOp(t, uint(DefaultWriteAttempts), cfg.WriteAttempts)
		test.EqOp(t, "work_queue_items", cfg.resolvedTable())
	})

	T.Run("leaves set knobs alone", func(t *testing.T) {
		t.Parallel()

		cfg := validConfig()
		cfg.Retention = time.Hour
		cfg.MaxClaimBatch = 7
		cfg.EnsureDefaults()

		test.EqOp(t, time.Hour, cfg.Retention)
		test.EqOp(t, 7, cfg.MaxClaimBatch)
	})

	// Zero is a meaningful MaxAttempts — unlimited — rather than an unset one,
	// so it must not be defaulted away.
	T.Run("does not default the attempt ceiling", func(t *testing.T) {
		t.Parallel()

		cfg := validConfig()
		cfg.EnsureDefaults()

		test.EqOp(t, uint(0), cfg.MaxAttempts)
		test.EqOp(t, 0, cfg.attemptCeiling())
	})

	T.Run("resolves the table under a namespace", func(t *testing.T) {
		t.Parallel()

		cfg := validConfig()
		cfg.TablePrefix = "ddb"
		cfg.EnsureDefaults()

		test.EqOp(t, "ddb_work_queue_items", cfg.resolvedTable())
	})
}

func TestConfig_ValidateWithContext(T *testing.T) {
	T.Parallel()

	T.Run("accepts a defaulted config", func(t *testing.T) {
		t.Parallel()

		cfg := validConfig()
		cfg.EnsureDefaults()

		test.NoError(t, cfg.ValidateWithContext(t.Context()))
	})

	T.Run("requires a name", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{}
		cfg.EnsureDefaults()

		test.Error(t, cfg.ValidateWithContext(t.Context()))
	})

	T.Run("rejects knobs the defaults would never produce", func(t *testing.T) {
		t.Parallel()

		for name, mutate := range map[string]func(*Config){
			"sub-second retention": func(cfg *Config) { cfg.Retention = time.Millisecond },
			"empty claim batch":    func(cfg *Config) { cfg.MaxClaimBatch = -1 },
			"empty reap batch":     func(cfg *Config) { cfg.ReapBatchSize = -1 },
		} {
			cfg := validConfig()
			cfg.EnsureDefaults()
			mutate(cfg)

			test.Error(t, cfg.ValidateWithContext(t.Context()), test.Sprintf("case %q", name))
		}
	})
}

func TestConfig_AttemptCeiling(T *testing.T) {
	T.Parallel()

	T.Run("passes an ordinary ceiling through", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{MaxAttempts: 5}

		test.EqOp(t, 5, cfg.attemptCeiling())
	})

	// Wrapping to a negative would read as "unlimited" on the way into the
	// predicate, silently turning the poison-item guard off — so it saturates.
	T.Run("saturates rather than wrapping", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{MaxAttempts: 1 << 40}

		must.EqOp(t, math.MaxInt32, cfg.attemptCeiling())
		test.True(t, cfg.attemptCeiling() > 0)
	})
}
