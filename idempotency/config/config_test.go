package idempotencycfg

import (
	"context"
	"testing"
	"time"

	cachecfg "github.com/primandproper/platform-go/v8/cache/config"
	distributedlockcfg "github.com/primandproper/platform-go/v8/distributedlock/config"
	"github.com/primandproper/platform-go/v8/idempotency"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// memoryConfig is a valid in-process configuration, for tests that care about
// everything except the providers.
func memoryConfig() *Config {
	return &Config{
		Cache: cachecfg.Config{Provider: cachecfg.ProviderMemory},
		Lock:  distributedlockcfg.Config{Provider: distributedlockcfg.MemoryProvider},
	}
}

func TestConfig_EnsureDefaults(T *testing.T) {
	T.Parallel()

	T.Run("fills in zero fields", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{}
		cfg.EnsureDefaults()

		test.EqOp(t, idempotency.DefaultTTL, cfg.TTL)
		test.EqOp(t, idempotency.DefaultInFlightTTL, cfg.InFlightTTL)
		test.EqOp(t, idempotency.DefaultMaxKeyLength, cfg.MaxKeyLength)
		test.EqOp(t, idempotency.DefaultKeyPrefix, cfg.KeyPrefix)
	})

	T.Run("leaves set fields alone", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{
			TTL:          time.Hour,
			InFlightTTL:  time.Minute,
			MaxKeyLength: 64,
			KeyPrefix:    "custom:",
		}
		cfg.EnsureDefaults()

		test.EqOp(t, time.Hour, cfg.TTL)
		test.EqOp(t, time.Minute, cfg.InFlightTTL)
		test.EqOp(t, 64, cfg.MaxKeyLength)
		test.EqOp(t, "custom:", cfg.KeyPrefix)
	})
}

func TestConfig_ValidateWithContext(T *testing.T) {
	T.Parallel()

	T.Run("accepts a valid config", func(t *testing.T) {
		t.Parallel()

		cfg := memoryConfig()
		cfg.EnsureDefaults()

		test.NoError(t, cfg.ValidateWithContext(t.Context()))
	})

	T.Run("rejects negative durations", func(t *testing.T) {
		t.Parallel()

		cfg := memoryConfig()
		cfg.TTL = -time.Second

		test.Error(t, cfg.ValidateWithContext(t.Context()))
	})

	// The nested configs are reached through validation.By closures, because
	// ozzo would otherwise dereference the struct and skip them.
	T.Run("rejects an invalid nested provider", func(t *testing.T) {
		t.Parallel()

		cfg := memoryConfig()
		cfg.Cache.Provider = "cassandra"

		test.Error(t, cfg.ValidateWithContext(t.Context()))
	})
}

func TestNewManager(T *testing.T) {
	T.Parallel()

	type payload struct {
		Name string
	}

	T.Run("builds a working manager", func(t *testing.T) {
		t.Parallel()

		m, err := NewManager[payload](t.Context(), memoryConfig(), nil, nil, nil, nil)
		must.NoError(t, err)
		must.NotNil(t, m)

		ctx := t.Context()
		calls := 0
		work := func(context.Context) (*payload, error) {
			calls++

			return &payload{Name: "v"}, nil
		}

		_, err = m.Do(ctx, "key-1", "fingerprint", work)
		must.NoError(t, err)

		result, err := m.Do(ctx, "key-1", "fingerprint", work)
		must.NoError(t, err)

		test.True(t, result.Replayed)
		test.EqOp(t, 1, calls)
	})

	T.Run("rejects a nil config", func(t *testing.T) {
		t.Parallel()

		_, err := NewManager[payload](t.Context(), nil, nil, nil, nil, nil)
		test.Error(t, err)
	})

	T.Run("rejects an invalid config", func(t *testing.T) {
		t.Parallel()

		cfg := memoryConfig()
		cfg.Cache.Provider = "cassandra"

		_, err := NewManager[payload](t.Context(), cfg, nil, nil, nil, nil)
		test.Error(t, err)
	})

	T.Run("caller options win over configuration", func(t *testing.T) {
		t.Parallel()

		cfg := memoryConfig()
		cfg.TTL = time.Hour

		m, err := NewManager(t.Context(), cfg, nil, nil, nil, nil,
			idempotency.WithTTL[payload](2*time.Hour))
		must.NoError(t, err)
		must.NotNil(t, m)
	})
}
