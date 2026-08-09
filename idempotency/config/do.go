package idempotencycfg

import (
	"context"

	"github.com/primandproper/platform-go/v10/database"
	"github.com/primandproper/platform-go/v10/idempotency"
	"github.com/primandproper/platform-go/v10/observability"

	"github.com/samber/do/v2"
)

// RegisterManager registers an *idempotency.Manager[T] with the injector. It
// is generic because a Manager stores results of one concrete type; each
// result type the application replays is registered separately.
//
// Prerequisites: *Config and database.Client must be registered in the
// injector before the Manager is invoked.
func RegisterManager[T any](i do.Injector) {
	do.Provide(i, func(i do.Injector) (*idempotency.Manager[T], error) {
		pillars, err := observability.InvokePillars(i)
		if err != nil {
			return nil, err
		}

		return NewManager[T](
			do.MustInvoke[context.Context](i),
			do.MustInvoke[*Config](i),
			do.MustInvoke[database.Client](i),
			WithPillars(pillars),
		)
	})
}
