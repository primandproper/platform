package sqlite

import (
	"context"

	"github.com/primandproper/platform-go/v10/database"
	"github.com/primandproper/platform-go/v10/observability"

	"github.com/samber/do/v2"
)

// RegisterDatabaseClient registers this implementation under two keys: its own
// type, *Client, and database.Client. Both resolve to the same client.
//
// The concrete key is what lets a caller who has chosen sqlite depend on the
// thing they chose. RawAccess is a pair of methods on *Client, so reaching the
// *sql.DB handles costs an interface assertion only for a caller who took the
// portable database.Client instead. The interface key is an alias rather than a
// second provider, so both callers share one client — which matters more here
// than elsewhere, sqlite's write side being a single serialized connection.
//
// Prerequisite: database.ClientConfig must be registered (e.g. via
// databasecfg.RegisterClientConfig).
func RegisterDatabaseClient(i do.Injector) {
	do.Provide(i, func(i do.Injector) (*Client, error) {
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

	// Cannot fail: *Client implements database.Client — the compiler says so at
	// the top of sqlite.go — and the service it aliases was just provided.
	do.MustAs[*Client, database.Client](i)
}
