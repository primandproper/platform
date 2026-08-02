package cachecfg

import (
	"context"
	"strings"
	"time"

	"github.com/primandproper/platform-go/v9/cache"
	"github.com/primandproper/platform-go/v9/cache/memory"
	"github.com/primandproper/platform-go/v9/cache/redis"
	circuitbreakingcfg "github.com/primandproper/platform-go/v9/circuitbreaking/config"
	"github.com/primandproper/platform-go/v9/errors"
	"github.com/primandproper/platform-go/v9/observability/logging"
	"github.com/primandproper/platform-go/v9/observability/metrics"
	"github.com/primandproper/platform-go/v9/observability/tracing"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

const (
	// ProviderMemory is the memory provider.
	ProviderMemory = "memory"
	// ProviderRedis is the redis provider.
	ProviderRedis = "redis"
)

type (
	// Config is the configuration for the cache.
	Config struct {
		Redis          *redis.Config             `env:",init"    envPrefix:"REDIS_"            json:"redis"                yaml:"redis"`
		Provider       string                    `env:"PROVIDER" json:"provider"               yaml:"provider"`
		CircuitBreaker circuitbreakingcfg.Config `env:",init"    envPrefix:"CIRCUIT_BREAKING_" json:"circuitBreakerConfig" yaml:"circuitBreakerConfig"`
		// Expiry is the default expiry for writes that don't specify one via
		// cache.WithExpiry; a non-positive value means entries never expire by
		// default.
		Expiry time.Duration `env:"EXPIRY" envDefault:"1h" json:"expiry" yaml:"expiry"`
		// JanitorInterval is how often the memory provider sweeps expired
		// entries. It is ignored by every other provider, which expire entries
		// in the backing store rather than in this process. A non-positive
		// value disables the sweep, leaving the memory provider's lazy
		// eviction as the only reclaim path — see memory.WithJanitor for why
		// that is rarely what a long-lived cache wants.
		JanitorInterval time.Duration `env:"JANITOR_INTERVAL" envDefault:"5m" json:"janitorInterval" yaml:"janitorInterval"`
	}
)

var _ validation.ValidatableWithContext = (*Config)(nil)

// ValidateWithContext validates a Config struct.
func (cfg *Config) ValidateWithContext(ctx context.Context) error {
	return validation.ValidateStructWithContext(ctx, cfg,
		validation.Field(&cfg.Provider, validation.In(ProviderMemory, ProviderRedis)),
		validation.Field(&cfg.Redis, validation.When(cfg.Provider == ProviderRedis, validation.Required)),
	)
}

// NewCache provides a Cache.
func NewCache[T any](ctx context.Context, cfg *Config, logger logging.Logger, tracerProvider tracing.TracerProvider, metricsProvider metrics.Provider) (cache.Cache[T], error) {
	switch strings.TrimSpace(strings.ToLower(cfg.Provider)) {
	case ProviderMemory:
		// The janitor is bound to the caller's context because cache.Cache has
		// no Close: the sweep stops when whatever scope owns this cache does.
		return memory.NewInMemoryCache[T](cfg.Expiry,
			memory.WithLogger(logger),
			memory.WithTracerProvider(tracerProvider),
			memory.WithMetricsProvider(metricsProvider),
			memory.WithJanitor(ctx, cfg.JanitorInterval))
	case ProviderRedis:
		cb, err := cfg.CircuitBreaker.NewCircuitBreaker(ctx, logger, metricsProvider)
		if err != nil {
			return nil, errors.Wrap(err, "initializing cache circuit breaker")
		}
		return redis.NewRedisCache[T](cfg.Redis, cfg.Expiry, cb,
			redis.WithLogger(logger),
			redis.WithTracerProvider(tracerProvider),
			redis.WithMetricsProvider(metricsProvider))
	default:
		return nil, errors.Newf("invalid cache provider: %q", cfg.Provider)
	}
}
