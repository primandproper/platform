package pprof

import (
	"context"
	"testing"

	"github.com/shoenig/test"
)

func TestConfig_ValidateWithContext(T *testing.T) {
	T.Parallel()

	ctx := context.Background()

	T.Run("valid config", func(t *testing.T) {
		t.Parallel()
		c := &Config{Port: 6060}
		test.NoError(t, c.ValidateWithContext(ctx))
	})

	T.Run("default port is valid", func(t *testing.T) {
		t.Parallel()
		c := &Config{Port: DefaultPort}
		test.NoError(t, c.ValidateWithContext(ctx))
	})
}
