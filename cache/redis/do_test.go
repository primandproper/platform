package redis

import (
	"testing"
	"time"

	"github.com/primandproper/platform-go/v10/cache"
	"github.com/primandproper/platform-go/v10/errors"
	loggingnoop "github.com/primandproper/platform-go/v10/observability/logging/noop"
	"github.com/primandproper/platform-go/v10/observability/metrics"
	metricsnoop "github.com/primandproper/platform-go/v10/observability/metrics/noop"
	tracingnoop "github.com/primandproper/platform-go/v10/observability/tracing/noop"

	"github.com/samber/do/v2"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func testRedisConfig() *Config {
	return &Config{Addresses: []string{"localhost:6379"}, Namespace: "do_test"}
}

func TestRegisterCache(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		i := do.New()
		do.ProvideValue(i, testRedisConfig())
		do.ProvideValue(i, loggingnoop.NewLogger())
		do.ProvideValue(i, tracingnoop.NewTracerProvider())
		do.ProvideValue(i, metricsnoop.NewMetricsProvider())

		RegisterCache[example](i, time.Minute, nil)

		c, err := do.Invoke[*Cache[example]](i)
		must.NoError(t, err)
		must.NotNil(t, c)
		test.EqOp(t, time.Minute, c.expiration)
		test.EqOp(t, "do_test", c.namespace)
	})

	// The concrete registration is the point of this function, so the interface
	// key has to be an alias rather than a second cache: two collaborators must
	// not each open their own connection pool.
	T.Run("both keys resolve to one cache", func(t *testing.T) {
		t.Parallel()

		i := do.New()
		do.ProvideValue(i, testRedisConfig())

		RegisterCache[example](i, time.Minute, nil)

		concrete, err := do.Invoke[*Cache[example]](i)
		must.NoError(t, err)

		iface, err := do.Invoke[cache.Cache[example]](i)
		must.NoError(t, err)

		test.EqOp(t, any(concrete), any(iface))
	})

	// The caller's options land after the pillars, which is what lets one
	// component opt out of observability the container does provide.
	T.Run("caller options override the pillars", func(t *testing.T) {
		t.Parallel()

		i := do.New()
		do.ProvideValue(i, testRedisConfig())
		do.ProvideValue(i, metricsnoop.NewMetricsProvider())

		RegisterCache[example](i, time.Minute, nil, WithMetricsProvider(nil))

		c, err := do.Invoke[*Cache[example]](i)
		must.NoError(t, err)
		test.Nil(t, c.metricsProvider)
	})

	T.Run("a construction failure reaches the caller", func(t *testing.T) {
		t.Parallel()

		i := do.New()
		do.ProvideValue(i, &Config{})

		RegisterCache[example](i, time.Minute, nil)

		_, err := do.Invoke[*Cache[example]](i)
		must.Error(t, err)
	})
}

// A registered observability provider that fails to build has to reach the
// caller. Treating it as absent would hand the component a noop and leave a
// misconfigured exporter looking configured — see observability.InvokePillars.
func TestRegisterCache_failingObservabilityIsAnError(T *testing.T) {
	T.Parallel()

	// Asserted by identity, not merely that some error came back: a missing
	// config would also fail, and would not exercise this branch.
	errBuild := errors.New("building the metrics provider")

	T.Run("RegisterCache", func(t *testing.T) {
		t.Parallel()

		i := do.New()
		do.ProvideValue(i, testRedisConfig())
		do.Provide(i, func(do.Injector) (metrics.Provider, error) {
			return nil, errBuild
		})

		RegisterCache[example](i, time.Minute, nil)

		_, err := do.Invoke[*Cache[example]](i)
		must.Error(t, err)
		test.ErrorIs(t, err, errBuild)
	})
}
