package circuitbreakingcfg

import (
	"testing"

	"github.com/primandproper/platform/circuitbreaking"
	"github.com/primandproper/platform/observability/logging"
	"github.com/primandproper/platform/observability/metrics"

	"github.com/samber/do/v2"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

//nolint:paralleltest // race condition in the core circuit breaker library, I think?
func TestRegisterCircuitBreaker(T *testing.T) {
	T.Run("standard", func(t *testing.T) {
		cfg := &Config{}
		cfg.EnsureDefaults()

		i := do.New()
		do.ProvideValue(i, t.Context())
		do.ProvideValue(i, logging.NewNoopLogger())
		do.ProvideValue(i, metrics.NewNoopMetricsProvider())
		do.ProvideValue(i, cfg)

		RegisterCircuitBreaker(i)

		cb, err := do.Invoke[circuitbreaking.CircuitBreaker](i)
		must.NoError(t, err)
		test.NotNil(t, cb)
	})
}
