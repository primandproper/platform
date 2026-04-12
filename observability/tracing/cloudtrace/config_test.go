package cloudtrace

import (
	"testing"

	"github.com/shoenig/test"
)

func TestCloudTraceConfig_ValidateWithContext(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		cfg := &Config{
			ProjectID: t.Name(),
		}

		test.NoError(t, cfg.ValidateWithContext(ctx))
	})
}
