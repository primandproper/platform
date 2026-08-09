package env

import (
	"testing"

	"github.com/primandproper/platform-go/v10/errors"
	loggingnoop "github.com/primandproper/platform-go/v10/observability/logging/noop"
	"github.com/primandproper/platform-go/v10/observability/metrics"
	metricsnoop "github.com/primandproper/platform-go/v10/observability/metrics/noop"
	tracingnoop "github.com/primandproper/platform-go/v10/observability/tracing/noop"
	"github.com/primandproper/platform-go/v10/secrets"

	"github.com/samber/do/v2"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestRegisterSecretSource(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		i := do.New()
		do.ProvideValue(i, loggingnoop.NewLogger())
		do.ProvideValue(i, tracingnoop.NewTracerProvider())
		do.ProvideValue(i, metricsnoop.NewMetricsProvider())

		RegisterSecretSource(i)

		source, err := do.Invoke[*SecretSource](i)
		must.NoError(t, err)
		must.NotNil(t, source)
	})

	// The concrete registration is the point of this function, so the interface
	// key has to be an alias rather than a second source.
	T.Run("both keys resolve to one source", func(t *testing.T) {
		t.Parallel()

		i := do.New()
		RegisterSecretSource(i)

		concrete, err := do.Invoke[*SecretSource](i)
		must.NoError(t, err)

		iface, err := do.Invoke[secrets.SecretSource](i)
		must.NoError(t, err)

		test.EqOp(t, any(concrete), any(iface))
	})

	// Absent means noop, so a container that wants no observability at all does
	// not have to register any to get a source.
	T.Run("wires up with no observability registered", func(t *testing.T) {
		t.Parallel()

		i := do.New()
		RegisterSecretSource(i)

		source, err := do.Invoke[*SecretSource](i)
		must.NoError(t, err)
		test.NotNil(t, source)
	})
}

// A registered observability provider that fails to build has to reach the
// caller. Treating it as absent would hand the component a noop and leave a
// misconfigured exporter looking configured — see observability.InvokePillars.
func TestRegisterSecretSource_failingObservabilityIsAnError(T *testing.T) {
	T.Parallel()

	// Asserted by identity, not merely that some error came back: several other
	// things could fail here, and would not exercise this branch.
	errBuild := errors.New("building the metrics provider")

	T.Run("RegisterSecretSource", func(t *testing.T) {
		t.Parallel()

		i := do.New()
		do.Provide(i, func(do.Injector) (metrics.Provider, error) {
			return nil, errBuild
		})

		RegisterSecretSource(i)

		_, err := do.Invoke[*SecretSource](i)
		must.Error(t, err)
		test.ErrorIs(t, err, errBuild)
	})
}
