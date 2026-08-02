package http

import (
	"context"

	"github.com/primandproper/platform-go/v9/observability"
	"github.com/primandproper/platform-go/v9/routing"

	"github.com/samber/do/v2"
)

// RegisterHTTPServer registers a Server with the injector.
// The serviceName parameter is passed directly rather than injected, since
// string is too generic a type to resolve unambiguously from the injector.
func RegisterHTTPServer(i do.Injector, serviceName string) {
	do.Provide[Server](i, func(i do.Injector) (Server, error) {
		pillars, err := observability.InvokePillars(i)
		if err != nil {
			return nil, err
		}

		cfg := do.MustInvoke[Config](i)

		return NewHTTPServer(
			context.Background(),
			&cfg,
			do.MustInvoke[*routing.Router](i),
			WithServiceName(serviceName),
			WithLogger(pillars.Logger),
			WithTracerProvider(pillars.TracerProvider),
		)
	})
}
