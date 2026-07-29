package cookiescfg

import (
	"github.com/primandproper/platform-go/v8/cookies"
	"github.com/primandproper/platform-go/v8/observability/tracing"

	"github.com/samber/do/v2"
)

// RegisterCookieManager registers a cookies.Manager with the injector.
func RegisterCookieManager(i do.Injector) {
	do.Provide[cookies.Manager](i, func(i do.Injector) (cookies.Manager, error) {
		return cookies.NewCookieManager(
			do.MustInvoke[*cookies.Config](i),
			cookies.WithTracerProvider(do.MustInvoke[tracing.TracerProvider](i)),
		)
	})
}
