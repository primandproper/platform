package httpclient

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

		test.EqOp(t, defaultTimeout, cfg.Timeout)
		test.EqOp(t, defaultMaxIdleConns, cfg.MaxIdleConns)
		test.EqOp(t, defaultMaxIdleConnsPerHost, cfg.MaxIdleConnsPerHost)
	})

	T.Run("preserves non-zero values", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{
			Timeout:             5 * time.Second,
			MaxIdleConns:        50,
			MaxIdleConnsPerHost: 25,
		}
		cfg.EnsureDefaults()

		test.EqOp(t, 5*time.Second, cfg.Timeout)
		test.EqOp(t, 50, cfg.MaxIdleConns)
		test.EqOp(t, 25, cfg.MaxIdleConnsPerHost)
	})
}

func TestConfig_ValidateWithContext(T *testing.T) {
	T.Parallel()

	T.Run("valid config", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		cfg := &Config{
			Timeout:             time.Second,
			MaxIdleConns:        10,
			MaxIdleConnsPerHost: 5,
		}

		err := cfg.ValidateWithContext(ctx)
		must.NoError(t, err)
	})

	T.Run("invalid timeout", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		cfg := &Config{
			Timeout:             0,
			MaxIdleConns:        10,
			MaxIdleConnsPerHost: 5,
		}

		err := cfg.ValidateWithContext(ctx)
		must.Error(t, err)
	})

	T.Run("invalid MaxIdleConns", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		cfg := &Config{
			Timeout:             time.Second,
			MaxIdleConns:        0,
			MaxIdleConnsPerHost: 5,
		}

		err := cfg.ValidateWithContext(ctx)
		must.Error(t, err)
	})

	T.Run("invalid MaxIdleConnsPerHost", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		cfg := &Config{
			Timeout:             time.Second,
			MaxIdleConns:        10,
			MaxIdleConnsPerHost: 0,
		}

		err := cfg.ValidateWithContext(ctx)
		must.Error(t, err)
	})
}
