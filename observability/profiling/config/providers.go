package profilingcfg

import (
	"context"

	"github.com/primandproper/platform-go/v11/observability/profiling"
)

// NewProfilingProvider provides a profiling.Provider from a config.
func NewProfilingProvider(ctx context.Context, c *Config, opts ...Option) (profiling.Provider, error) {
	return c.NewProfilingProvider(ctx, opts...)
}
