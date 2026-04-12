package config

import (
	"testing"

	"github.com/primandproper/platform/cryptography/encryption"
	"github.com/primandproper/platform/observability/logging"
	"github.com/primandproper/platform/observability/tracing"

	"github.com/samber/do/v2"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestRegisterEncryptorDecryptor(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		i := do.New()
		do.ProvideValue(i, &Config{Provider: ProviderAES})
		do.ProvideValue(i, tracing.NewNoopTracerProvider())
		do.ProvideValue(i, logging.NewNoopLogger())
		do.ProvideValue(i, []byte(testKey))

		RegisterEncryptorDecryptor(i)

		encDec, err := do.Invoke[encryption.EncryptorDecryptor](i)
		must.NoError(t, err)
		test.NotNil(t, encDec)
	})
}
