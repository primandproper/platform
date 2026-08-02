package partitionedcfg

import (
	"context"

	"github.com/primandproper/platform-go/v9/circuitbreaking"
	circuitbreakingcfg "github.com/primandproper/platform-go/v9/circuitbreaking/config"
	"github.com/primandproper/platform-go/v9/circuitbreaking/partitioned"
	"github.com/primandproper/platform-go/v9/errors"
	"github.com/primandproper/platform-go/v9/observability/logging"

	validation "github.com/go-ozzo/ozzo-validation/v4"
	"go.opentelemetry.io/otel/attribute"
)

const (
	// partitionAttributeKey is the metric attribute used to distinguish breakers that share counter names.
	partitionAttributeKey = "partition"

	// globalPartition is the partition attribute value used for the shared fallback breaker.
	globalPartition = "global"
)

// Config configures a partitioned (keyed) circuit breaker.
type Config struct {
	Keys []string                  `env:"KEYS"  json:"circuitBreakerKeys" yaml:"circuitBreakerKeys"`
	Base circuitbreakingcfg.Config `env:",init" envPrefix:"BASE_"         json:"base"               yaml:"base"`
}

// EnsureDefaults ensures the config has sane defaults.
func (cfg *Config) EnsureDefaults() {
	cfg.Base.EnsureDefaults()
}

// ValidateWithContext validates a Config struct.
func (cfg *Config) ValidateWithContext(ctx context.Context) error {
	if err := cfg.Base.ValidateWithContext(ctx); err != nil {
		return err
	}

	return validation.ValidateStructWithContext(ctx, cfg,
		validation.Field(&cfg.Keys, validation.Each(validation.Required)),
	)
}

// NewKeyedCircuitBreaker provides a KeyedCircuitBreaker.
func (cfg *Config) NewKeyedCircuitBreaker(ctx context.Context, opts ...Option) (partitioned.KeyedCircuitBreaker, error) {
	o := newOptions(opts)
	logger, metricsProvider := o.logger, o.metricsProvider

	if cfg == nil {
		return nil, errors.ErrNilInputParameter
	}

	logger = logging.EnsureLogger(logger)

	// Defaults are applied before validating, matching the base package: an unset
	// Base.Name is the common case, and validating first turned it into a noop
	// keyed breaker — protection that looks wired and does nothing.
	cfg.EnsureDefaults()

	if err := cfg.ValidateWithContext(ctx); err != nil {
		return nil, errors.Wrap(err, "validating keyed circuit breaker config")
	}

	global, err := cfg.Base.NewCircuitBreaker(ctx, circuitbreakingcfg.WithLogger(logger), circuitbreakingcfg.WithMetricsProvider(metricsProvider), circuitbreakingcfg.WithMetricAttributes(attribute.String(partitionAttributeKey, globalPartition)))
	if err != nil {
		return nil, errors.Wrap(err, "providing global circuit breaker")
	}

	breakers := make(map[string]circuitbreaking.CircuitBreaker, len(cfg.Keys))
	for _, key := range cfg.Keys {
		cb, cbErr := cfg.Base.NewCircuitBreaker(ctx, circuitbreakingcfg.WithLogger(logger), circuitbreakingcfg.WithMetricsProvider(metricsProvider), circuitbreakingcfg.WithMetricAttributes(attribute.String(partitionAttributeKey, key)))
		if cbErr != nil {
			return nil, errors.Wrapf(cbErr, "providing circuit breaker for key %q", key)
		}

		breakers[key] = cb
	}

	return partitioned.NewKeyedCircuitBreaker(global, breakers), nil
}

// NewKeyedCircuitBreaker provides a KeyedCircuitBreaker from config.
func NewKeyedCircuitBreaker(ctx context.Context, cfg *Config, opts ...Option) (partitioned.KeyedCircuitBreaker, error) {
	return cfg.NewKeyedCircuitBreaker(ctx, opts...)
}
