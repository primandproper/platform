package redis

import (
	"testing"

	"github.com/shoenig/test"
)

func TestConfig_ValidateWithContext(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		cfg := &Config{
			Username:       t.Name(),
			Password:       t.Name(),
			QueueAddresses: []string{t.Name()},
		}

		test.NoError(t, cfg.ValidateWithContext(ctx))
	})
}
