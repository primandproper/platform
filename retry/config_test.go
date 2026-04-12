package retry

import (
	"context"
	"testing"
	"time"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestConfig_EnsureDefaults(T *testing.T) {
	T.Parallel()

	T.Run("sets defaults for zero values", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{}
		cfg.EnsureDefaults()

		test.EqOp(t, uint(3), cfg.MaxAttempts)
		test.EqOp(t, 100*time.Millisecond, cfg.InitialDelay)
		test.EqOp(t, 5*time.Second, cfg.MaxDelay)
		test.EqOp(t, 2.0, cfg.Multiplier)
	})

	T.Run("preserves non-zero values", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{
			MaxAttempts:  7,
			InitialDelay: 1 * time.Second,
			MaxDelay:     10 * time.Second,
			Multiplier:   3.0,
		}
		cfg.EnsureDefaults()

		test.EqOp(t, uint(7), cfg.MaxAttempts)
		test.EqOp(t, 1*time.Second, cfg.InitialDelay)
		test.EqOp(t, 10*time.Second, cfg.MaxDelay)
		test.EqOp(t, 3.0, cfg.Multiplier)
	})
}

func TestConfig_ValidateWithContext(T *testing.T) {
	T.Parallel()

	T.Run("valid config", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		cfg := &Config{
			MaxAttempts:  1,
			InitialDelay: time.Millisecond,
			MaxDelay:     time.Second,
			Multiplier:   2.0,
		}

		err := cfg.ValidateWithContext(ctx)
		must.NoError(t, err)
	})

	T.Run("invalid MaxAttempts", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		cfg := &Config{
			MaxAttempts:  0,
			InitialDelay: time.Millisecond,
			MaxDelay:     time.Second,
			Multiplier:   2.0,
		}

		err := cfg.ValidateWithContext(ctx)
		must.Error(t, err)
	})

	T.Run("invalid InitialDelay", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		cfg := &Config{
			MaxAttempts:  1,
			InitialDelay: 0,
			MaxDelay:     time.Second,
			Multiplier:   2.0,
		}

		err := cfg.ValidateWithContext(ctx)
		must.Error(t, err)
	})

	T.Run("invalid Multiplier", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		cfg := &Config{
			MaxAttempts:  1,
			InitialDelay: time.Millisecond,
			MaxDelay:     time.Second,
			Multiplier:   0.5,
		}

		err := cfg.ValidateWithContext(ctx)
		must.Error(t, err)
	})
}
