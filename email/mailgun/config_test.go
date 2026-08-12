package mailgun

import (
	"testing"

	"github.com/shoenig/test"
)

func TestConfig_ValidateWithContext(T *testing.T) {
	T.Parallel()

	T.Run("with populated config", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{
			PrivateAPIKey: t.Name(),
			Domain:        t.Name(),
		}

		test.NoError(t, cfg.ValidateWithContext(t.Context()))
	})

	T.Run("with empty config", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{}

		test.Error(t, cfg.ValidateWithContext(t.Context()))
	})

	// An empty base URL means the SDK's default host, so it is not a failure.
	T.Run("with an absent base URL", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{
			PrivateAPIKey: t.Name(),
			Domain:        t.Name(),
		}

		test.NoError(t, cfg.ValidateWithContext(t.Context()))
	})

	T.Run("with the EU base URL", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{
			PrivateAPIKey: t.Name(),
			Domain:        t.Name(),
			BaseURL:       BaseURLEU,
		}

		test.NoError(t, cfg.ValidateWithContext(t.Context()))
	})

	// Caught at startup rather than at the first send, which is where a typo in
	// a region URL would otherwise surface.
	T.Run("with a relative base URL", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{
			PrivateAPIKey: t.Name(),
			Domain:        t.Name(),
			BaseURL:       "api.eu.mailgun.net/v3",
		}

		test.Error(t, cfg.ValidateWithContext(t.Context()))
	})

	T.Run("with an unparseable base URL", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{
			PrivateAPIKey: t.Name(),
			Domain:        t.Name(),
			BaseURL:       string([]byte{0x7f}),
		}

		test.Error(t, cfg.ValidateWithContext(t.Context()))
	})
}
