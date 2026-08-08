package tracingcfg

import (
	"context"

	"github.com/primandproper/platform-go/v10/internal/injection"
	"github.com/primandproper/platform-go/v10/observability/logging"
	"github.com/primandproper/platform-go/v10/observability/tracing"

	"github.com/samber/do/v2"
)

// RegisterTracerProvider registers a tracing.TracerProvider with the injector.
//
// The logger is looked up optionally rather than through do.MustInvoke: a
// container that registers no logger still gets a tracer provider, which just
// says nothing about how it was set up.
func RegisterTracerProvider(i do.Injector) {
	do.Provide[tracing.TracerProvider](i, func(i do.Injector) (tracing.TracerProvider, error) {
		logger, err := injection.InvokeOptional[logging.Logger](i)
		if err != nil {
			return nil, err
		}

		return NewTracerProvider(
			do.MustInvoke[context.Context](i),
			do.MustInvoke[*Config](i),
			WithLogger(logger),
		)
	})
}
