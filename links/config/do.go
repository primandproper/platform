package linkscfg

import (
	"context"

	"github.com/primandproper/platform-go/v14/database"
	"github.com/primandproper/platform-go/v14/internal/injection"
	"github.com/primandproper/platform-go/v14/links"
	"github.com/primandproper/platform-go/v14/observability"

	"github.com/samber/do/v2"
)

// RegisterMinter registers a *links.Minter with the injector.
//
// Prerequisites: context.Context and *Config must be registered. A
// database.Client is resolved only if one is registered, so a container running
// the cache provider against a memory or redis lock needs none — and one whose
// registered client fails to build still hears about it.
func RegisterMinter(i do.Injector) {
	do.Provide(i, func(i do.Injector) (*links.Minter, error) {
		pillars, err := observability.InvokePillars(i)
		if err != nil {
			return nil, err
		}

		db, err := injection.InvokeOptional[database.Client](i)
		if err != nil {
			return nil, err
		}

		return NewMinter(
			do.MustInvoke[context.Context](i),
			do.MustInvoke[*Config](i),
			db,
			WithPillars(pillars),
		)
	})
}
