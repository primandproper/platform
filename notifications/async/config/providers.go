package asynccfg

import (
	"github.com/primandproper/platform-go/v7/notifications/async"
	"github.com/primandproper/platform-go/v7/observability/logging"
	"github.com/primandproper/platform-go/v7/observability/metrics"
	"github.com/primandproper/platform-go/v7/observability/tracing"
)

// NewAsyncNotifier provides an AsyncNotifier from a config.
func NewAsyncNotifier(cfg *Config, logger logging.Logger, tracerProvider tracing.TracerProvider, metricsProvider metrics.Provider) (async.AsyncNotifier, error) {
	return cfg.NewAsyncNotifier(logger, tracerProvider, metricsProvider)
}
