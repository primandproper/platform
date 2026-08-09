package redis

import (
	"testing"

	cbnoop "github.com/primandproper/platform-go/v10/circuitbreaking/noop"
	"github.com/primandproper/platform-go/v10/distributedlock"
	"github.com/primandproper/platform-go/v10/errors"
	loggingnoop "github.com/primandproper/platform-go/v10/observability/logging/noop"
	"github.com/primandproper/platform-go/v10/observability/metrics"
	metricsnoop "github.com/primandproper/platform-go/v10/observability/metrics/noop"
	tracingnoop "github.com/primandproper/platform-go/v10/observability/tracing/noop"

	"github.com/samber/do/v2"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// doTestConfig is addressed at a redis nobody has to be running: NewRedisLocker
// dials lazily, so every assertion here is about wiring, not connectivity.
func doTestConfig() *Config {
	return &Config{Addresses: []string{"localhost:6379"}, KeyPrefix: "do_test:"}
}

func TestRegisterLocker(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		i := do.New()
		do.ProvideValue(i, doTestConfig())
		do.ProvideValue(i, loggingnoop.NewLogger())
		do.ProvideValue(i, tracingnoop.NewTracerProvider())
		do.ProvideValue(i, metricsnoop.NewMetricsProvider())

		RegisterLocker(i, cbnoop.NewCircuitBreaker())

		l, err := do.Invoke[*Locker](i)
		must.NoError(t, err)
		must.NotNil(t, l)
		test.EqOp(t, "do_test:", l.keyPrefix)
	})

	// The concrete registration is the point of this function, so the interface
	// key has to be an alias rather than a second locker: two collaborators
	// must not each open their own connection pool.
	T.Run("both keys resolve to one locker", func(t *testing.T) {
		t.Parallel()

		i := do.New()
		do.ProvideValue(i, doTestConfig())

		RegisterLocker(i, cbnoop.NewCircuitBreaker())

		concrete, err := do.Invoke[*Locker](i)
		must.NoError(t, err)

		iface, err := do.Invoke[distributedlock.Locker](i)
		must.NoError(t, err)

		test.EqOp(t, any(concrete), any(iface))
	})

	T.Run("a construction failure reaches the caller", func(t *testing.T) {
		t.Parallel()

		i := do.New()
		do.ProvideValue(i, &Config{})

		RegisterLocker(i, cbnoop.NewCircuitBreaker())

		_, err := do.Invoke[*Locker](i)
		must.Error(t, err)
		test.ErrorIs(t, err, distributedlock.ErrNilConfig)
	})
}

// A registered observability provider that fails to build has to reach the
// caller. Treating it as absent would hand the component a noop and leave a
// misconfigured exporter looking configured — see observability.InvokePillars.
func TestRegisterLocker_failingObservabilityIsAnError(T *testing.T) {
	T.Parallel()

	// Asserted by identity, not merely that some error came back: a missing
	// config would also fail, and would not exercise this branch.
	errBuild := errors.New("building the metrics provider")

	T.Run("RegisterLocker", func(t *testing.T) {
		t.Parallel()

		i := do.New()
		do.ProvideValue(i, doTestConfig())
		do.Provide(i, func(do.Injector) (metrics.Provider, error) {
			return nil, errBuild
		})

		RegisterLocker(i, cbnoop.NewCircuitBreaker())

		_, err := do.Invoke[*Locker](i)
		must.Error(t, err)
		test.ErrorIs(t, err, errBuild)
	})
}
