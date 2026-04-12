package config

import (
	"testing"

	"github.com/primandproper/platform/capitalism"
	"github.com/primandproper/platform/capitalism/stripe"
	"github.com/primandproper/platform/observability/logging"
	"github.com/primandproper/platform/observability/tracing"

	"github.com/samber/do/v2"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestRegisterPaymentManager(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		i := do.New()
		do.ProvideValue(i, logging.NewNoopLogger())
		do.ProvideValue(i, tracing.NewNoopTracerProvider())
		do.ProvideValue(i, &Config{
			Provider: StripeProvider,
			Stripe:   &stripe.Config{APIKey: t.Name()},
		})

		RegisterPaymentManager(i)

		pm, err := do.Invoke[capitalism.PaymentManager](i)
		must.NoError(t, err)
		test.NotNil(t, pm)
	})
}
