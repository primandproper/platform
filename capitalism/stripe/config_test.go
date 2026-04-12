package stripe

import (
	"testing"

	"github.com/shoenig/test"
)

func TestStripeConfig_ValidateWithContext(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		cfg := &Config{
			APIKey: "blah",
		}

		test.NoError(t, cfg.ValidateWithContext(ctx))
	})

	T.Run("with missing API key", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		cfg := &Config{
			APIKey: "",
		}

		test.Error(t, cfg.ValidateWithContext(ctx))
	})
}
