package qrcodes

import (
	"github.com/primandproper/platform-go/v9/observability/logging"
	"github.com/primandproper/platform-go/v9/observability/tracing"

	"github.com/samber/do/v2"
)

// RegisterBuilder registers a Builder with the injector.
func RegisterBuilder(i do.Injector) {
	do.Provide[Builder](i, func(i do.Injector) (Builder, error) {
		return NewBuilder(
			do.MustInvoke[Issuer](i),
			WithLogger(do.MustInvoke[logging.Logger](i)),
			WithTracerProvider(do.MustInvoke[tracing.TracerProvider](i)),
		), nil
	})
}
