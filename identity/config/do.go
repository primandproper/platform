package identitycfg

import (
	"context"

	"github.com/primandproper/platform-go/v14/database"
	"github.com/primandproper/platform-go/v14/identity"
	"github.com/primandproper/platform-go/v14/observability"

	"github.com/samber/do/v2"
)

// RegisterStore registers an identity.Store with the injector.
//
// Prerequisites: *Config and database.Client must be registered in the injector
// before the Store is invoked.
func RegisterStore(i do.Injector) {
	do.Provide(i, func(i do.Injector) (identity.Store, error) {
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
}
