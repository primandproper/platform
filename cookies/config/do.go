package cookiescfg

import (
	"github.com/primandproper/platform-go/v10/cookies"
	"github.com/primandproper/platform-go/v10/observability"

	"github.com/samber/do/v2"
)

// RegisterCookieManager registers a cookies.Manager with the injector.
func RegisterCookieManager(i do.Injector) {
	do.Provide(i, func(i do.Injector) (cookies.Manager, error) {
		pillars, err := observability.InvokePillars(i)
		if err != nil {
			return nil, err
		}

		manager, err := cookies.NewCookieManager(
			do.MustInvoke[*cookies.Config](i),
			cookies.WithLogger(pillars.Logger),
			cookies.WithTracerProvider(pillars.TracerProvider),
		)
		if err != nil {
			return nil, err
		}

		return manager, nil
	})
}
