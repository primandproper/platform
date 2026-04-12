package qrcodes

import (
	"testing"

	"github.com/primandproper/platform/observability/logging"
	"github.com/primandproper/platform/observability/tracing"

	"github.com/samber/do/v2"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestRegisterBuilder(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		i := do.New()
		do.ProvideValue(i, Issuer(t.Name()))
		do.ProvideValue(i, tracing.NewNoopTracerProvider())
		do.ProvideValue(i, logging.NewNoopLogger())

		RegisterBuilder(i)

		b, err := do.Invoke[Builder](i)
		must.NoError(t, err)
		test.NotNil(t, b)
	})
}
