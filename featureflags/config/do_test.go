package featureflagscfg

import (
	"net/http"
	"testing"

	"github.com/primandproper/platform/featureflags"
	"github.com/primandproper/platform/observability/logging"
	"github.com/primandproper/platform/observability/metrics"
	"github.com/primandproper/platform/observability/tracing"

	"github.com/samber/do/v2"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestRegisterFeatureFlagManager(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		i := do.New()
		do.ProvideValue(i, t.Context())
		do.ProvideValue(i, logging.NewNoopLogger())
		do.ProvideValue(i, tracing.NewNoopTracerProvider())
		do.ProvideValue(i, metrics.NewNoopMetricsProvider())
		do.ProvideValue(i, http.DefaultClient)
		do.ProvideValue(i, &Config{})

		RegisterFeatureFlagManager(i)

		ffm, err := do.Invoke[featureflags.FeatureFlagManager](i)
		must.NoError(t, err)
		test.NotNil(t, ffm)
	})
}
