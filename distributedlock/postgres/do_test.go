package postgres

import (
	"testing"

	cbnoop "github.com/primandproper/platform-go/v10/circuitbreaking/noop"
	"github.com/primandproper/platform-go/v10/database"
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

// provideLockerDeps registers everything both Register functions here invoke,
// with every pillar present, so a test that cares about one of them can drop it
// and know the drop is what changed.
func provideLockerDeps(i do.Injector) {
	do.ProvideValue(i, &Config{})
	do.ProvideValue[database.Client](i, &testDBClient{})
	do.ProvideValue(i, loggingnoop.NewLogger())
	do.ProvideValue(i, tracingnoop.NewTracerProvider())
	do.ProvideValue(i, metricsnoop.NewMetricsProvider())
}

func TestRegisterLocker(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		i := do.New()
		provideLockerDeps(i)

		RegisterLocker(i, cbnoop.NewCircuitBreaker())

		l, err := do.Invoke[*Locker](i)
		must.NoError(t, err)
		must.NotNil(t, l)
	})

	// The concrete registration is the point of this function, so the interface
	// key has to be an alias rather than a second locker: each held lock pins a
	// write-pool connection, and two lockers would pin two sets.
	T.Run("both keys resolve to one locker", func(t *testing.T) {
		t.Parallel()

		i := do.New()
		provideLockerDeps(i)

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
		provideLockerDeps(i)
		do.OverrideValue[database.Client](i, nil)

		RegisterLocker(i, cbnoop.NewCircuitBreaker())

		_, err := do.Invoke[*Locker](i)
		must.Error(t, err)
		test.ErrorIs(t, err, distributedlock.ErrNilDatabaseClient)
	})
}

func TestRegisterScopedLocker(T *testing.T) {
	T.Parallel()

	T.Run("both keys resolve to one locker", func(t *testing.T) {
		t.Parallel()

		i := do.New()
		provideLockerDeps(i)

		RegisterScopedLocker(i, cbnoop.NewCircuitBreaker())

		concrete, err := do.Invoke[*ScopedLocker](i)
		must.NoError(t, err)

		iface, err := do.Invoke[distributedlock.ScopedLocker](i)
		must.NoError(t, err)

		test.EqOp(t, any(concrete), any(iface))
	})
}

// A registered observability provider that fails to build has to reach the
// caller. Treating it as absent would hand the component a noop and leave a
// misconfigured exporter looking configured — see observability.InvokePillars.
func TestRegister_failingObservabilityIsAnError(T *testing.T) {
	T.Parallel()

	// Asserted by identity, not merely that some error came back: a missing
	// config would also fail, and would not exercise this branch.
	errBuild := errors.New("building the metrics provider")

	T.Run("RegisterLocker", func(t *testing.T) {
		t.Parallel()

		i := do.New()
		do.Provide(i, func(do.Injector) (metrics.Provider, error) {
			return nil, errBuild
		})

		RegisterLocker(i, cbnoop.NewCircuitBreaker())

		_, err := do.Invoke[*Locker](i)
		must.Error(t, err)
		test.ErrorIs(t, err, errBuild)
	})

	T.Run("RegisterScopedLocker", func(t *testing.T) {
		t.Parallel()

		i := do.New()
		do.Provide(i, func(do.Injector) (metrics.Provider, error) {
			return nil, errBuild
		})

		RegisterScopedLocker(i, cbnoop.NewCircuitBreaker())

		_, err := do.Invoke[*ScopedLocker](i)
		must.Error(t, err)
		test.ErrorIs(t, err, errBuild)
	})
}
