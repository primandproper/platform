package posthog

import (
	"testing"

	"github.com/shoenig/test/must"
)

func TestConfig_ValidateWithContext(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{APIKey: t.Name()}

		must.NoError(t, cfg.ValidateWithContext(t.Context()))
	})

	T.Run("with empty API key", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{}

		must.Error(t, cfg.ValidateWithContext(t.Context()))
	})
}
