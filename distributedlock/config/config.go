package distributedlockcfg

import (
	"context"
	"slices"
	"strings"

	circuitbreakingcfg "github.com/primandproper/platform-go/v10/circuitbreaking/config"
	"github.com/primandproper/platform-go/v10/database"
	"github.com/primandproper/platform-go/v10/distributedlock"
	"github.com/primandproper/platform-go/v10/distributedlock/memory"
	"github.com/primandproper/platform-go/v10/distributedlock/noop"
	pglock "github.com/primandproper/platform-go/v10/distributedlock/postgres"
	redislock "github.com/primandproper/platform-go/v10/distributedlock/redis"
	"github.com/primandproper/platform-go/v10/errors"

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
	Redis          *redislock.Config         `env:",init"    envPrefix:"REDIS_"            json:"redis,omitempty"               yaml:"redis,omitempty"`
	Postgres       *pglock.Config            `env:",init"    envPrefix:"POSTGRES_"         json:"postgres,omitempty"            yaml:"postgres,omitempty"`
	Provider       string                    `env:"PROVIDER" json:"provider,omitempty"     yaml:"provider,omitempty"`
	CircuitBreaker circuitbreakingcfg.Config `env:",init"    envPrefix:"CIRCUIT_BREAKING_" json:"circuitBreakerConfig,omitzero" yaml:"circuitBreakerConfig,omitempty"`
}

var _ validation.ValidatableWithContext = (*Config)(nil)

// ValidateWithContext validates a Config struct. Provider is required: the noop
// locker is reachable only by naming it.
//
// The sub-config for a provider that was not selected is skipped rather than
// merely unguarded: ozzo validates any non-nil pointer to a Validatable once a
// field's rules have run, and `env:",init"` leaves every sub-config non-nil. A
// validation.When guard alone stops the Required rule and nothing else, so Redis
// addresses were demanded of the memory and noop lockers. Releasing the zero
// sub-configs instead would not do: both carry envDefault fields, so neither is
// zero once the environment has been parsed.
//
// The selection is read normalized, matching dispatch: a "REDIS" that
// knownProvider accepts and NewLocker dispatches on would otherwise skip the
// very block it is about to use.
func (cfg *Config) ValidateWithContext(ctx context.Context) error {
	provider := strings.TrimSpace(strings.ToLower(cfg.Provider))

	return validation.ValidateStructWithContext(ctx, cfg,
		validation.Field(&cfg.Provider, validation.Required, validation.By(func(any) error {
			if !knownProvider(cfg.Provider) {
				return errors.Wrapf(errors.ErrUnknownProvider, "distributedlock provider %q", cfg.Provider)
			}

			return nil
		})),
		validation.Field(&cfg.Redis, validation.Skip.When(provider != RedisProvider), validation.Required),
		validation.Field(&cfg.Postgres, validation.Skip.When(provider != PostgresProvider), validation.Required),
	)
}

// NewLocker constructs a distributedlock.Locker for the configured provider.
// The db argument is required only when Provider is PostgresProvider; pass nil
// otherwise. An unknown or empty provider is an error.
func NewLocker(
	ctx context.Context,
	cfg *Config,
	db database.Client,
	opts ...Option,
) (distributedlock.Locker, error) {
	o := newOptions(opts)
	logger, tracerProvider, metricsProvider := o.logger, o.tracerProvider, o.metricsProvider

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

	circuitBreaker, err := circuitbreakingcfg.NewCircuitBreaker(ctx, &cfg.CircuitBreaker, circuitbreakingcfg.WithLogger(logger), circuitbreakingcfg.WithMetricsProvider(metricsProvider))
	if err != nil {
		return nil, errors.Wrap(err, "initializing distributedlock circuit breaker")
	}

	// Each provider is built into a variable and returned only once its error is
	// known to be nil. The provider constructors return their own concrete types,
	// so returning one straight from the constructor would convert a nil *Locker
	// into a non-nil distributedlock.Locker on the error path.
	switch strings.TrimSpace(strings.ToLower(cfg.Provider)) {
	case RedisProvider:
		l, lockerErr := redislock.NewRedisLocker(cfg.Redis, circuitBreaker,
			redislock.WithLogger(logger),
			redislock.WithTracerProvider(tracerProvider),
			redislock.WithMetricsProvider(metricsProvider))
		if lockerErr != nil {
			return nil, lockerErr
		}

		return l, nil
	case PostgresProvider:
		l, lockerErr := pglock.NewPostgresLocker(cfg.Postgres, db, circuitBreaker,
			pglock.WithLogger(logger),
			pglock.WithTracerProvider(tracerProvider),
			pglock.WithMetricsProvider(metricsProvider))
		if lockerErr != nil {
			return nil, lockerErr
		}

		return l, nil
	case MemoryProvider:
		l, lockerErr := memory.NewLocker(
			memory.WithLogger(logger),
			memory.WithTracerProvider(tracerProvider),
			memory.WithMetricsProvider(metricsProvider))
		if lockerErr != nil {
			return nil, lockerErr
		}

		return l, nil
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
	db database.Client,
	opts ...Option,
) (distributedlock.ScopedLocker, error) {
	o := newOptions(opts)
	logger, tracerProvider, metricsProvider := o.logger, o.tracerProvider, o.metricsProvider

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
		circuitBreaker, err := circuitbreakingcfg.NewCircuitBreaker(ctx, &cfg.CircuitBreaker, circuitbreakingcfg.WithLogger(logger), circuitbreakingcfg.WithMetricsProvider(metricsProvider))
		if err != nil {
			return nil, errors.Wrap(err, "initializing distributedlock circuit breaker")
		}

		// Built into a variable and returned only once its error is known to be
		// nil: NewPostgresScopedLocker returns *pglock.ScopedLocker, so returning
		// it straight through would convert a nil pointer into a non-nil
		// distributedlock.ScopedLocker on the error path.
		l, err := pglock.NewPostgresScopedLocker(cfg.Postgres, db, circuitBreaker,
			pglock.WithLogger(logger),
			pglock.WithTracerProvider(tracerProvider),
			pglock.WithMetricsProvider(metricsProvider))
		if err != nil {
			return nil, err
		}

		return l, nil
	case RedisProvider, MemoryProvider:
		locker, err := NewLocker(ctx, cfg, db,
			WithLogger(logger),
			WithTracerProvider(tracerProvider),
			WithMetricsProvider(metricsProvider))
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
