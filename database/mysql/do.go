package mysql

import (
	"context"

	"github.com/primandproper/platform-go/v10/database"
	"github.com/primandproper/platform-go/v10/observability"

	"github.com/samber/do/v2"
)

// RegisterDatabaseClient registers a database.Client with the injector.
// Prerequisite: database.ClientConfig must be registered (e.g. via databasecfg.RegisterClientConfig).
func RegisterDatabaseClient(i do.Injector) {
	do.Provide[database.Client](i, func(i do.Injector) (database.Client, error) {
		pillars, err := observability.InvokePillars(i)
		if err != nil {
			return nil, err
		}

		return NewDatabaseClient(
			do.MustInvoke[context.Context](i),
			do.MustInvoke[database.ClientConfig](i),
			WithLogger(pillars.Logger),
			WithTracerProvider(pillars.TracerProvider),
			WithMetricsProvider(pillars.MetricsProvider),
		)
	})
}
