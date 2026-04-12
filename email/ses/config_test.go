package ses

import (
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestConfig_ValidateWithContext(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{
			Region: "us-east-1",
		}

		must.NoError(t, cfg.ValidateWithContext(t.Context()))
	})

	T.Run("with missing region", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{}

		err := cfg.ValidateWithContext(t.Context())
		must.Error(t, err)
		test.StrContains(t, err.Error(), "region")
	})
}
