package ratelimitingcfg

import (
	"context"

	"github.com/primandproper/platform-go/v10/observability"
	"github.com/primandproper/platform-go/v10/ratelimiting"

	"github.com/samber/do/v2"
)

// RegisterRateLimiter registers a RateLimiter with the injector.
func RegisterRateLimiter(i do.Injector) {
	do.Provide(i, func(i do.Injector) (ratelimiting.RateLimiter, error) {
		pillars, err := observability.InvokePillars(i)
		if err != nil {
			return nil, err
		}

		return NewRateLimiter(
			context.Background(),
			do.MustInvoke[*Config](i),
			WithPillars(pillars),
		)
	})
}
