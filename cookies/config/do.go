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

		return cookies.NewCookieManager(
			do.MustInvoke[*cookies.Config](i),
			cookies.WithTracerProvider(pillars.TracerProvider),
		)
	})
}
