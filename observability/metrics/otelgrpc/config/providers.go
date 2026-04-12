package config

import (
	"context"

	"github.com/primandproper/platform/observability/logging"
	"github.com/primandproper/platform/observability/metrics"
	"github.com/primandproper/platform/observability/metrics/otelgrpc"
)

// ProvideMetricsProvider provides a metrics.Provider from the config.
func ProvideMetricsProvider(ctx context.Context, logger logging.Logger, cfg *Config) (metrics.Provider, error) {
	return otelgrpc.ProvideMetricsProvider(ctx, logger, cfg.ServiceName, cfg.Otel)
}
