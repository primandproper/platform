package postgres

import (
	"testing"

	"github.com/shoenig/test/must"
)

func TestConfig_ValidateWithContext(T *testing.T) {
	T.Parallel()

	T.Run("happy path zero namespace", func(t *testing.T) {
		t.Parallel()
		cfg := &Config{}
		must.NoError(t, cfg.ValidateWithContext(t.Context()))
	})

	T.Run("happy path explicit namespace", func(t *testing.T) {
		t.Parallel()
		cfg := &Config{Namespace: 42}
		must.NoError(t, cfg.ValidateWithContext(t.Context()))
	})
}
