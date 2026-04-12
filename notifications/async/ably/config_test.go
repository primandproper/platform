package ably

import (
	"testing"

	"github.com/shoenig/test"
)

func TestConfig_ValidateWithContext(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{
			APIKey: "test.key:secret",
		}

		test.NoError(t, cfg.ValidateWithContext(t.Context()))
	})

	T.Run("missing api key", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{}

		test.Error(t, cfg.ValidateWithContext(t.Context()))
	})
}
