package distributedlockcfg

import (
	"context"
	"slices"
	"strings"

	circuitbreakingcfg "github.com/primandproper/platform-go/v9/circuitbreaking/config"
	"github.com/primandproper/platform-go/v9/database"
	"github.com/primandproper/platform-go/v9/distributedlock"
	"github.com/primandproper/platform-go/v9/distributedlock/memory"
	"github.com/primandproper/platform-go/v9/distributedlock/noop"
	pglock "github.com/primandproper/platform-go/v9/distributedlock/postgres"
	redislock "github.com/primandproper/platform-go/v9/distributedlock/redis"
	"github.com/primandproper/platform-go/v9/errors"
	"github.com/primandproper/platform-go/v9/observability/logging"
	"github.com/primandproper/platform-go/v9/observability/metrics"
	"github.com/primandproper/platform-go/v9/observability/tracing"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

const (
	// RedisProvider selects the redis-backed distributedlock.Locker implementation.
	RedisProvider = "redis"
	// PostgresProvider selects the postgres-backed distributedlock.Locker implementation.
	PostgresProvider = "postgres"
	// MemoryProvider selects the in-memory distributedlock.Locker implementation.
	MemoryProvider = "memory"
	// NoopProvider selects the no-op distributedlock.Locker implementation,
	// whose Acquire always succeeds. It must be chosen deliberately: an unset or
	// unrecognized provider is an error, because silently removing mutual
	// exclusion looks exactly like a system that never contends.
	NoopProvider = "noop"
)

// providers are every provider this package implements. The dispatch switches
// and ValidateWithContext all read it, so they cannot drift apart.
var providers = []string{RedisProvider, PostgresProvider, MemoryProvider, NoopProvider}

// knownProvider reports whether p names an implementation, ignoring case and
// surrounding space, exactly as the dispatch switches do.
func knownProvider(p string) bool {
	return slices.Contains(providers, strings.TrimSpace(strings.ToLower(p)))
}

// Config dispatches to a distributedlock provider implementation.
type Config struct {
	_              struct{}                  `json:"-"       yaml:"-"`
	Redis          *redislock.Config         `env:",init"    envPrefix:"REDIS_"            json:"redis"                yaml:"redis"`
	Postgres       *pglock.Config            `env:",init"    envPrefix:"POSTGRES_"         json:"postgres"             yaml:"postgres"`
	Provider       string                    `env:"PROVIDER" json:"provider"               yaml:"provider"`
	CircuitBreaker circuitbreakingcfg.Config `env:",init"    envPrefix:"CIRCUIT_BREAKING_" json:"circuitBreakerConfig" yaml:"circuitBreakerConfig"`
}

var _ validation.ValidatableWithContext = (*Config)(nil)

// ValidateWithContext validates a Config struct. Provider is required: the noop
// locker is reachable only by naming it.
func (cfg *Config) ValidateWithContext(ctx context.Context) error {
	return validation.ValidateStructWithContext(ctx, cfg,
		validation.Field(&cfg.Provider, validation.Required, validation.By(func(any) error {
			if !knownProvider(cfg.Provider) {
				return errors.Wrapf(errors.ErrUnknownProvider, "distributedlock provider %q", cfg.Provider)
			}

			return nil
		})),
		validation.Field(&cfg.Redis, validation.When(cfg.Provider == RedisProvider, validation.Required)),
		validation.Field(&cfg.Postgres, validation.When(cfg.Provider == PostgresProvider, validation.Required)),
	)
}

// NewLocker constructs a distributedlock.Locker for the configured provider.
// The db argument is required only when Provider is PostgresProvider; pass nil
// otherwise. An unknown or empty provider is an error.
func NewLocker(
	ctx context.Context,
	cfg *Config,
	logger logging.Logger,
	tracerProvider tracing.TracerProvider,
	metricsProvider metrics.Provider,
	db database.Client,
) (distributedlock.Locker, error) {
	if cfg == nil {
		return nil, distributedlock.ErrNilConfig
	}

	// Checked before the rest of the config so an unrecognized provider reports
	// ErrUnknownProvider rather than a downstream consequence of it.
	if !knownProvider(cfg.Provider) {
		return nil, errors.Wrapf(errors.ErrUnknownProvider, "distributedlock provider %q", cfg.Provider)
	}

	if err := cfg.ValidateWithContext(ctx); err != nil {
		return nil, errors.Wrap(err, "validating distributedlock config")
	}

	circuitBreaker, err := circuitbreakingcfg.NewCircuitBreaker(ctx, &cfg.CircuitBreaker, logger, metricsProvider)
	if err != nil {
		return nil, errors.Wrap(err, "initializing distributedlock circuit breaker")
	}

	switch strings.TrimSpace(strings.ToLower(cfg.Provider)) {
	case RedisProvider:
		return redislock.NewRedisLocker(cfg.Redis, circuitBreaker,
			redislock.WithLogger(logger),
			redislock.WithTracerProvider(tracerProvider),
			redislock.WithMetricsProvider(metricsProvider))
	case PostgresProvider:
		return pglock.NewPostgresLocker(cfg.Postgres, db, circuitBreaker,
			pglock.WithLogger(logger),
			pglock.WithTracerProvider(tracerProvider),
			pglock.WithMetricsProvider(metricsProvider))
	case MemoryProvider:
		return memory.NewLocker(
			memory.WithLogger(logger),
			memory.WithTracerProvider(tracerProvider),
			memory.WithMetricsProvider(metricsProvider))
	case NoopProvider:
		return noop.NewLocker(), nil
	default:
		return nil, errors.Wrapf(errors.ErrUnknownProvider, "distributedlock provider %q", cfg.Provider)
	}
}

// NewScopedLocker constructs a distributedlock.ScopedLocker for the configured
// provider. The postgres provider gets the native transaction-scoped
// implementation (server-side waiting, no TTL); redis and memory wrap their
// Locker in the generic scoped adapter with its defaults. As with NewLocker,
// db is required only for PostgresProvider, and an unknown or empty provider
// is an error.
func NewScopedLocker(
	ctx context.Context,
	cfg *Config,
	logger logging.Logger,
	tracerProvider tracing.TracerProvider,
	metricsProvider metrics.Provider,
	db database.Client,
) (distributedlock.ScopedLocker, error) {
	if cfg == nil {
		return nil, distributedlock.ErrNilConfig
	}

	// Checked before the rest of the config so an unrecognized provider reports
	// ErrUnknownProvider rather than a downstream consequence of it.
	if !knownProvider(cfg.Provider) {
		return nil, errors.Wrapf(errors.ErrUnknownProvider, "distributedlock provider %q", cfg.Provider)
	}

	if err := cfg.ValidateWithContext(ctx); err != nil {
		return nil, errors.Wrap(err, "validating distributedlock config")
	}

	switch strings.TrimSpace(strings.ToLower(cfg.Provider)) {
	case PostgresProvider:
		circuitBreaker, err := circuitbreakingcfg.NewCircuitBreaker(ctx, &cfg.CircuitBreaker, logger, metricsProvider)
		if err != nil {
			return nil, errors.Wrap(err, "initializing distributedlock circuit breaker")
		}

		return pglock.NewPostgresScopedLocker(cfg.Postgres, db, circuitBreaker,
			pglock.WithLogger(logger),
			pglock.WithTracerProvider(tracerProvider),
			pglock.WithMetricsProvider(metricsProvider))
	case RedisProvider, MemoryProvider:
		locker, err := NewLocker(ctx, cfg, logger, tracerProvider, metricsProvider, db)
		if err != nil {
			return nil, err
		}

		return distributedlock.NewScopedLocker(locker,
			distributedlock.WithLogger(logger),
			distributedlock.WithTracerProvider(tracerProvider),
			distributedlock.WithMetricsProvider(metricsProvider))
	case NoopProvider:
		return noop.NewScopedLocker(), nil
	default:
		return nil, errors.Wrapf(errors.ErrUnknownProvider, "distributedlock provider %q", cfg.Provider)
	}
}
