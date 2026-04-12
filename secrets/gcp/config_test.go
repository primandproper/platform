package gcp

import (
	"context"
	"testing"

	"github.com/shoenig/test/must"
)

func TestConfig_ValidateWithContext(T *testing.T) {
	T.Parallel()

	T.Run("valid", func(t *testing.T) {
		t.Parallel()
		cfg := &Config{ProjectID: "my-project"}
		must.NoError(t, cfg.ValidateWithContext(context.Background()))
	})

	T.Run("invalid missing ProjectID", func(t *testing.T) {
		t.Parallel()
		cfg := &Config{ProjectID: ""}
		must.Error(t, cfg.ValidateWithContext(context.Background()))
	})
}
