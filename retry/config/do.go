package retrycfg

import (
	"context"

	"github.com/primandproper/platform-go/v10/observability"
	"github.com/primandproper/platform-go/v10/retry"

	"github.com/samber/do/v2"
)

// RegisterPolicy registers a retry.Policy with the injector.
func RegisterPolicy(i do.Injector) {
	do.Provide(i, func(i do.Injector) (retry.Policy, error) {
		pillars, err := observability.InvokePillars(i)
		if err != nil {
			return nil, err
		}

		return NewPolicy(
			do.MustInvoke[context.Context](i),
			do.MustInvoke[*Config](i),
			WithPillars(pillars),
		)
	})
}
