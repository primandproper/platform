package tracingcfg

import (
	"context"

	"github.com/primandproper/platform/observability/logging"
	"github.com/primandproper/platform/observability/tracing"
)

// ProvideTracerProvider provides a TracerProvider from config.
func ProvideTracerProvider(ctx context.Context, c *Config, l logging.Logger) (traceProvider tracing.TracerProvider, err error) {
	return c.ProvideTracerProvider(ctx, l)
}
