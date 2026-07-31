package http

import (
	"github.com/primandproper/platform-go/v9/observability/logging"
	"github.com/primandproper/platform-go/v9/observability/tracing"
	"github.com/primandproper/platform-go/v9/routing"

	"github.com/samber/do/v2"
)

// RegisterHTTPServer registers a Server with the injector.
// The serviceName parameter is passed directly rather than injected, since
// string is too generic a type to resolve unambiguously from the injector.
func RegisterHTTPServer(i do.Injector, serviceName string) {
	do.Provide[Server](i, func(i do.Injector) (Server, error) {
		cfg := do.MustInvoke[Config](i)

		return NewHTTPServer(
			&cfg,
			do.MustInvoke[*routing.Router](i),
			serviceName,
			WithLogger(do.MustInvoke[logging.Logger](i)),
			WithTracerProvider(do.MustInvoke[tracing.TracerProvider](i)),
		)
	})
}
