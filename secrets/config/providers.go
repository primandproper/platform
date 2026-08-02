package secretscfg

import (
	"context"

	"github.com/primandproper/platform-go/v9/errors"
	"github.com/primandproper/platform-go/v9/observability/logging"
	"github.com/primandproper/platform-go/v9/observability/metrics"
	"github.com/primandproper/platform-go/v9/observability/tracing"
	"github.com/primandproper/platform-go/v9/secrets"
	"github.com/primandproper/platform-go/v9/secrets/env"
)

// NewSecretSource provides a SecretSource from config.
func NewSecretSource(ctx context.Context, cfg *Config, logger logging.Logger, tracerProvider tracing.TracerProvider, metricsProvider metrics.Provider) (secrets.SecretSource, error) {
	if cfg == nil {
		return env.NewSecretSource(env.WithLogger(logger), env.WithTracerProvider(tracerProvider), env.WithMetricsProvider(metricsProvider))
	}
	source, err := cfg.NewSecretSource(ctx, logger, tracerProvider, metricsProvider)
	if err != nil {
		return nil, errors.Wrap(err, "provide secret source")
	}
	return source, nil
}
