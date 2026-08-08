package secretscfg

import (
	"context"

	"github.com/primandproper/platform-go/v10/errors"
	"github.com/primandproper/platform-go/v10/secrets"
	"github.com/primandproper/platform-go/v10/secrets/env"
)

// NewSecretSource provides a SecretSource from config.
func NewSecretSource(ctx context.Context, cfg *Config, opts ...Option) (secrets.SecretSource, error) {
	if cfg == nil {
		o := newOptions(opts)

		return env.NewSecretSource(env.WithLogger(o.logger), env.WithTracerProvider(o.tracerProvider), env.WithMetricsProvider(o.metricsProvider))
	}

	source, err := cfg.NewSecretSource(ctx, opts...)
	if err != nil {
		return nil, errors.Wrap(err, "provide secret source")
	}

	return source, nil
}
