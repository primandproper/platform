package asynccfg

import (
	"context"

	"github.com/primandproper/platform-go/v9/notifications/async"
	"github.com/primandproper/platform-go/v9/observability/logging"
	"github.com/primandproper/platform-go/v9/observability/metrics"
	"github.com/primandproper/platform-go/v9/observability/tracing"
)

// NewAsyncNotifier provides an AsyncNotifier from a config.
func NewAsyncNotifier(ctx context.Context, cfg *Config, logger logging.Logger, tracerProvider tracing.TracerProvider, metricsProvider metrics.Provider) (async.AsyncNotifier, error) {
	return cfg.NewAsyncNotifier(logger, tracerProvider, metricsProvider)
}
