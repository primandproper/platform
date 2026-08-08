package tracingcfg

import (
	"context"

	"github.com/primandproper/platform-go/v10/observability/tracing"
)

// NewTracerProvider provides a tracing.TracerProvider from a config.
func NewTracerProvider(ctx context.Context, c *Config, opts ...Option) (traceProvider tracing.TracerProvider, err error) {
	return c.NewTracerProvider(ctx, opts...)
}
