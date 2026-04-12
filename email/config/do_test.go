package emailcfg

import (
	"net/http"
	"testing"

	"github.com/primandproper/platform/email"
	"github.com/primandproper/platform/email/sendgrid"
	"github.com/primandproper/platform/observability/logging"
	"github.com/primandproper/platform/observability/metrics"
	"github.com/primandproper/platform/observability/tracing"

	"github.com/samber/do/v2"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestRegisterEmailer(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{
			Provider: ProviderSendgrid,
			Sendgrid: &sendgrid.Config{APIToken: t.Name()},
		}
		cfg.CircuitBreaker.Name = t.Name()

		i := do.New()
		do.ProvideValue(i, t.Context())
		do.ProvideValue(i, logging.NewNoopLogger())
		do.ProvideValue(i, tracing.NewNoopTracerProvider())
		do.ProvideValue[metrics.Provider](i, metrics.NewNoopMetricsProvider())
		do.ProvideValue(i, &http.Client{})
		do.ProvideValue(i, cfg)

		RegisterEmailer(i)

		emailer, err := do.Invoke[email.Emailer](i)
		must.NoError(t, err)
		test.NotNil(t, emailer)
	})
}
