package notificationscfg

import (
	"context"

	"github.com/primandproper/platform-go/v14/database"
	"github.com/primandproper/platform-go/v14/notifications"
	"github.com/primandproper/platform-go/v14/observability"

	"github.com/samber/do/v2"
)

// RegisterStore registers the notifications store with the injector, under
// three keys: the concrete *notifications.SQLStore, and the two seams it
// satisfies — notifications.Inbox and notifications.Registry.
//
// The three are one store rather than three. The interfaces are registered as
// narrowings of the concrete registration, so a container that invokes the
// inbox and the registry gets the same value, holding one connection and one
// set of instruments.
//
// Registering the registry under its interface is what closes this package's
// feedback loop from configuration: mobilecfg.RegisterPushSender resolves a
// notifications.Registry optionally and, finding one, hands it to the sender as
// its token invalidator — so a container carrying both halves prunes tokens the
// providers have permanently rejected, and one carrying only the sender behaves
// exactly as it did before.
//
// Prerequisites: *Config and database.Client must be registered in the injector
// before the store is invoked.
func RegisterStore(i do.Injector) {
	do.Provide(i, func(i do.Injector) (*notifications.SQLStore, error) {
		pillars, err := observability.InvokePillars(i)
		if err != nil {
			return nil, err
		}

		return NewStore(
			do.MustInvoke[context.Context](i),
			do.MustInvoke[*Config](i),
			do.MustInvoke[database.Client](i),
			WithPillars(pillars),
		)
	})

	// The two narrowings, each returning only once the store's error is known to
	// be nil: handing a nil *SQLStore straight back would make a non-nil
	// interface holding a nil pointer, and a caller testing the result against
	// nil would find a store that panics on first use.
	do.Provide(i, func(i do.Injector) (notifications.Inbox, error) {
		store, err := do.Invoke[*notifications.SQLStore](i)
		if err != nil {
			return nil, err
		}

		return store, nil
	})

	do.Provide(i, func(i do.Injector) (notifications.Registry, error) {
		store, err := do.Invoke[*notifications.SQLStore](i)
		if err != nil {
			return nil, err
		}

		return store, nil
	})
}
