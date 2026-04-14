package cookies

import (
	"testing"
	"time"

	"github.com/shoenig/test"
)

func TestConfig_ValidateWithContext(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{
			CookieName:            t.Name(),
			Base64EncodedHashKey:  t.Name(),
			Base64EncodedBlockKey: t.Name(),
			Lifetime:              24 * time.Hour,
		}

		test.NoError(t, cfg.ValidateWithContext(t.Context()))
	})

	T.Run("with lifetime below minimum", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{
			CookieName:            t.Name(),
			Base64EncodedHashKey:  t.Name(),
			Base64EncodedBlockKey: t.Name(),
			Lifetime:              1 * time.Minute,
		}

		test.Error(t, cfg.ValidateWithContext(t.Context()))
	})

	T.Run("with missing name", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{
			Base64EncodedHashKey:  t.Name(),
			Base64EncodedBlockKey: t.Name(),
		}

		test.Error(t, cfg.ValidateWithContext(t.Context()))
	})

	T.Run("with missing hash key", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{
			Base64EncodedBlockKey: t.Name(),
		}

		test.Error(t, cfg.ValidateWithContext(t.Context()))
	})

	T.Run("with missing block key", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{
			Base64EncodedHashKey: t.Name(),
		}

		test.Error(t, cfg.ValidateWithContext(t.Context()))
	})
}
