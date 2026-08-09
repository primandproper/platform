package postgres

import (
	"context"
	"testing"

	"github.com/primandproper/platform-go/v10/database"
	loggingnoop "github.com/primandproper/platform-go/v10/observability/logging/noop"
	"github.com/primandproper/platform-go/v10/observability/metrics"
	tracingnoop "github.com/primandproper/platform-go/v10/observability/tracing/noop"

	"github.com/samber/do/v2"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestRegisterDatabaseClient(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		i := do.New()
		do.ProvideValue[context.Context](i, t.Context())
		do.ProvideValue(i, loggingnoop.NewLogger())
		do.ProvideValue(i, tracingnoop.NewTracerProvider())
		do.ProvideValue[metrics.Provider](i, nil)
		do.ProvideValue[database.ClientConfig](i, &testClientConfig{
			connectionString: "user=test password=test database=test host=localhost port=5432",
			maxPingAttempts:  1,
		})

		RegisterDatabaseClient(i)

		client, err := do.Invoke[database.Client](i)
		must.NoError(t, err)
		test.NotNil(t, client)
	})

	// The concrete registration is what lets a caller depend on this driver
	// rather than on database.Client, so the interface key has to be an alias
	// rather than a second client: two collaborators must not each open their
	// own connection pool.
	T.Run("both keys resolve to one client", func(t *testing.T) {
		t.Parallel()

		i := do.New()
		do.ProvideValue[context.Context](i, t.Context())
		do.ProvideValue[database.ClientConfig](i, &testClientConfig{
			connectionString: "user=test password=test database=test host=localhost port=5432",
			maxPingAttempts:  1,
		})

		RegisterDatabaseClient(i)

		concrete, err := do.Invoke[*Client](i)
		must.NoError(t, err)
		t.Cleanup(func() { must.NoError(t, concrete.Close()) })

		iface, err := do.Invoke[database.Client](i)
		must.NoError(t, err)

		test.EqOp(t, any(concrete), any(iface))
	})
}
