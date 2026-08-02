package metricscfg

import (
	"context"

	"github.com/primandproper/platform-go/v9/observability/logging"
	"github.com/primandproper/platform-go/v9/observability/metrics"
)

// NewMetricsProvider provides a metrics.Provider from config.
func NewMetricsProvider(ctx context.Context, c *Config, logger logging.Logger) (metrics.Provider, error) {
	return c.NewMetricsProvider(ctx, logger)
}
