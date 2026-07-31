package mysql

import (
	"context"

	"github.com/primandproper/platform-go/v9/database"
	"github.com/primandproper/platform-go/v9/observability/logging"
	"github.com/primandproper/platform-go/v9/observability/metrics"
	"github.com/primandproper/platform-go/v9/observability/tracing"

	"github.com/samber/do/v2"
)

// RegisterDatabaseClient registers a database.Client with the injector.
// Prerequisite: database.ClientConfig must be registered (e.g. via databasecfg.RegisterClientConfig).
func RegisterDatabaseClient(i do.Injector) {
	do.Provide[database.Client](i, func(i do.Injector) (database.Client, error) {
		return NewDatabaseClient(
			do.MustInvoke[context.Context](i),
			do.MustInvoke[database.ClientConfig](i),
			WithLogger(do.MustInvoke[logging.Logger](i)),
			WithTracerProvider(do.MustInvoke[tracing.TracerProvider](i)),
			WithMetricsProvider(do.MustInvoke[metrics.Provider](i)),
		)
	})
}
