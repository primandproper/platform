package tracingcfg

import (
	"context"

	"github.com/primandproper/platform-go/v12/observability/tracing"
)

// NewTracerProvider provides a tracing.Provider from a config.
func NewTracerProvider(ctx context.Context, c *Config, opts ...Option) (traceProvider tracing.Provider, err error) {
	return c.NewTracerProvider(ctx, opts...)
}
