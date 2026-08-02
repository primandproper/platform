package http

import (
	"testing"
	"time"

	"github.com/shoenig/test"
)

func TestConfig_Validate(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		cfg := &Config{
			StartupDeadline: time.Second,
			Port:            8080,
		}

		test.NoError(t, cfg.ValidateWithContext(ctx))
	})

	// Port 0 asks the OS for an ephemeral port, which is a real configuration
	// and the one the tests in this package bind on.
	T.Run("accepts an unset port", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		cfg := &Config{
			StartupDeadline: time.Second,
		}

		test.NoError(t, cfg.ValidateWithContext(ctx))
	})

	// A zero StartupDeadline means the bind is unbounded, which is the default.
	T.Run("accepts an unset startup deadline", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		cfg := &Config{
			Port: 8080,
		}

		test.NoError(t, cfg.ValidateWithContext(ctx))
	})

	T.Run("rejects a negative timeout", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		cfg := &Config{
			Port:        8080,
			ReadTimeout: -time.Second,
		}

		test.Error(t, cfg.ValidateWithContext(ctx))
	})

	T.Run("returns error with partial apple app site association config", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		cfg := &Config{
			StartupDeadline:         time.Second,
			Port:                    8080,
			AppleAppSiteAssociation: &AppleAppSiteAssociationConfig{TeamID: "ABCD1234XY"},
		}

		test.Error(t, cfg.ValidateWithContext(ctx))
	})

	T.Run("accepts the zero config", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		cfg := &Config{}

		test.NoError(t, cfg.ValidateWithContext(ctx))
	})
}
