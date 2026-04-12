package ssm

import (
	"context"
	"testing"

	"github.com/shoenig/test/must"
)

func TestConfig_ValidateWithContext(T *testing.T) {
	T.Parallel()

	T.Run("valid", func(t *testing.T) {
		t.Parallel()
		cfg := &Config{Region: "us-east-1"}
		must.NoError(t, cfg.ValidateWithContext(context.Background()))
	})

	T.Run("invalid missing Region", func(t *testing.T) {
		t.Parallel()
		cfg := &Config{Region: ""}
		must.Error(t, cfg.ValidateWithContext(context.Background()))
	})
}
