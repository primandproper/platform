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
		Redis    *redis.Config `env:",init"    envPrefix:"REDIS_"        json:"redis,omitempty"    yaml:"redis,omitempty"`
		Provider string        `env:"PROVIDER" json:"provider,omitempty" yaml:"provider,omitempty"`
		// EvictionPolicy names which entry the memory provider drops once
		// MaxEntries is reached: "least_recently_used" (alias "lru") or
		// "oldest_written" (alias "fifo"). It is read only when MaxEntries is
		// positive, so the default here costs an unbounded cache nothing.
		EvictionPolicy string                    `env:"EVICTION_POLICY" envDefault:"least_recently_used" json:"evictionPolicy,omitempty"      yaml:"evictionPolicy,omitempty"`
		CircuitBreaker circuitbreakingcfg.Config `env:",init"           envPrefix:"CIRCUIT_BREAKING_"    json:"circuitBreakerConfig,omitzero" yaml:"circuitBreakerConfig,omitempty"`
		// Expiry is the default expiry for writes that don't specify one via
		// cache.WithExpiry; a non-positive value means entries never expire by
		// default.
		Expiry time.Duration `env:"EXPIRY" envDefault:"1h" json:"expiry,omitempty" yaml:"expiry,omitempty"`
		// JanitorInterval is how often the memory provider sweeps expired
		// entries. It is ignored by every other provider, which expire entries
		// in the backing store rather than in this process. A non-positive
		// value disables the sweep, leaving the memory provider's lazy
		// eviction as the only reclaim path — see memory.WithJanitor for why
		// that is rarely what a long-lived cache wants.
		JanitorInterval time.Duration `env:"JANITOR_INTERVAL" envDefault:"5m" json:"janitorInterval,omitempty" yaml:"janitorInterval,omitempty"`
		// MaxEntries bounds how many entries the memory provider holds,
		// dropping one per EvictionPolicy whenever a write would exceed it. It
		// is ignored by every other provider, which bound their own storage.
		// A non-positive value leaves the cache bounded only by expiry — see
		// memory.WithMaxEntries for when that is not enough.
		MaxEntries int `env:"MAX_ENTRIES" json:"maxEntries,omitempty" yaml:"maxEntries,omitempty"`
	}
)

var _ validation.ValidatableWithContext = (*Config)(nil)

// ValidateWithContext validates a Config struct.
//
// The sub-config for a provider that was not selected is skipped rather than
// merely unguarded: ozzo validates any non-nil pointer to a Validatable once a
// field's rules have run, and `env:",init"` leaves every sub-config non-nil. A
// validation.When guard alone stops the Required rule and nothing else, so
// Redis' own rules were enforced and the memory provider could not load.
func (cfg *Config) ValidateWithContext(ctx context.Context) error {
	return validation.ValidateStructWithContext(ctx, cfg,
		validation.Field(&cfg.Provider, validation.In(ProviderMemory, ProviderRedis)),
		validation.Field(&cfg.Redis, validation.Skip.When(cfg.Provider != ProviderRedis), validation.Required),
		validation.Field(&cfg.EvictionPolicy,
			validation.Skip.When(cfg.Provider != ProviderMemory || cfg.MaxEntries <= 0),
			validation.By(validEvictionPolicy)),
	)
}

// validEvictionPolicy reports whether the configured policy names one the
// memory provider implements, so a typo fails validation rather than
// construction. The check is skipped for a cache that will not be bounded,
// which is why the field's default costs an unbounded cache nothing.
func validEvictionPolicy(value any) error {
	name, ok := value.(string)
	if !ok {
		return errors.Newf("expected a string eviction policy, got %T", value)
	}

	_, err := memory.ParseEvictionPolicy(name)

	return err
}

// NewCache provides a Cache.
func NewCache[T any](ctx context.Context, cfg *Config, opts ...Option) (cache.Cache[T], error) {
	o := newOptions(opts)

	switch strings.TrimSpace(strings.ToLower(cfg.Provider)) {
	case ProviderMemory:
		// The janitor is bound to the caller's context because cache.Cache has
		// no Close: the sweep stops when whatever scope owns this cache does.
		memoryOpts := []memory.Option{
			memory.WithLogger(o.logger),
			memory.WithTracerProvider(o.tracerProvider),
			memory.WithMetricsProvider(o.metricsProvider),
			memory.WithJanitor(ctx, cfg.JanitorInterval),
		}

		// Resolved only for a cache that will be bounded, so an unbounded one
		// is never failed by a policy it would not have consulted.
		if cfg.MaxEntries > 0 {
			policy, err := memory.ParseEvictionPolicy(cfg.EvictionPolicy)
			if err != nil {
				return nil, errors.Wrap(err, "resolving cache eviction policy")
			}

			memoryOpts = append(memoryOpts, memory.WithMaxEntries(cfg.MaxEntries, policy))
		}

		return memory.NewInMemoryCache[T](cfg.Expiry, memoryOpts...)
	case ProviderRedis:
		cb, err := cfg.CircuitBreaker.NewCircuitBreaker(ctx,
			circuitbreakingcfg.WithLogger(o.logger),
			circuitbreakingcfg.WithMetricsProvider(o.metricsProvider))
		if err != nil {
			return nil, errors.Wrap(err, "initializing cache circuit breaker")
		}
		return redis.NewRedisCache[T](cfg.Redis, cfg.Expiry, cb,
			redis.WithLogger(o.logger),
			redis.WithTracerProvider(o.tracerProvider),
			redis.WithMetricsProvider(o.metricsProvider))
	default:
		return nil, errors.Newf("invalid cache provider: %q", cfg.Provider)
	}
}
