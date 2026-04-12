package encoding

import (
	"testing"

	"github.com/primandproper/platform/observability/logging"
	"github.com/primandproper/platform/observability/tracing"

	"github.com/samber/do/v2"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestRegisterServerEncoderDecoder(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		i := do.New()
		do.ProvideValue(i, Config{ContentType: "application/json"})
		do.ProvideValue(i, logging.NewNoopLogger())
		do.ProvideValue(i, tracing.NewNoopTracerProvider())

		RegisterServerEncoderDecoder(i)

		ct, err := do.Invoke[ContentType](i)
		must.NoError(t, err)
		test.NotNil(t, ct)

		sed, err := do.Invoke[ServerEncoderDecoder](i)
		must.NoError(t, err)
		test.NotNil(t, sed)
	})
}
