package tokenscfg

import (
	"encoding/base64"
	"testing"

	"github.com/primandproper/platform/authentication/tokens"
	loggingnoop "github.com/primandproper/platform/observability/logging/noop"
	tracingnoop "github.com/primandproper/platform/observability/tracing/noop"
	"github.com/primandproper/platform/random"

	"github.com/samber/do/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProvideTokenIssuer(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		cfg := &Config{
			Provider:                ProviderJWT,
			Issuer:                  t.Name(),
			Audience:                t.Name(),
			Base64EncodedSigningKey: base64.URLEncoding.EncodeToString(random.MustGenerateRawBytes(ctx, 32)),
		}

		issuer, err := ProvideTokenIssuer(cfg, loggingnoop.NewLogger(), tracingnoop.NewTracerProvider())
		require.NoError(t, err)
		assert.NotNil(t, issuer)
	})
}

func TestRegisterTokenIssuer(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		cfg := &Config{
			Provider:                ProviderJWT,
			Issuer:                  t.Name(),
			Audience:                t.Name(),
			Base64EncodedSigningKey: base64.URLEncoding.EncodeToString(random.MustGenerateRawBytes(ctx, 32)),
		}

		i := do.New()
		do.ProvideValue(i, loggingnoop.NewLogger())
		do.ProvideValue(i, tracingnoop.NewTracerProvider())
		do.ProvideValue(i, cfg)

		RegisterTokenIssuer(i)

		issuer, err := do.Invoke[tokens.Issuer](i)
		require.NoError(t, err)
		assert.NotNil(t, issuer)
	})
}
