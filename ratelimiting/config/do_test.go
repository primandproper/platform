package ratelimitingcfg

import (
	"testing"

	"github.com/primandproper/platform-go/v9/observability/logging"
	loggingnoop "github.com/primandproper/platform-go/v9/observability/logging/noop"
	"github.com/primandproper/platform-go/v9/observability/metrics"
	"github.com/primandproper/platform-go/v9/observability/tracing"
	tracingnoop "github.com/primandproper/platform-go/v9/observability/tracing/noop"
	"github.com/primandproper/platform-go/v9/ratelimiting"

	"github.com/samber/do/v2"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestRegisterRateLimiter(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		i := do.New()
		do.ProvideValue[logging.Logger](i, loggingnoop.NewLogger())
		do.ProvideValue[tracing.TracerProvider](i, tracingnoop.NewTracerProvider())
		do.ProvideValue[metrics.Provider](i, nil)
		do.ProvideValue(i, &Config{Provider: ProviderNoop})

		RegisterRateLimiter(i)

		limiter, err := do.Invoke[ratelimiting.RateLimiter](i)
		must.NoError(t, err)
		test.NotNil(t, limiter)
	})
}
