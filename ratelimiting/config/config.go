package ratelimitingcfg

import (
	"context"
	"slices"

	"github.com/primandproper/platform-go/v10/errors"
	"github.com/primandproper/platform-go/v10/internal/cfgnorm"
	"github.com/primandproper/platform-go/v10/ratelimiting"
	"github.com/primandproper/platform-go/v10/ratelimiting/noop"
	redisrl "github.com/primandproper/platform-go/v10/ratelimiting/redis"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

const (
	// ProviderMemory selects the in-process limiter.
	ProviderMemory = "memory"
	// ProviderNoop selects a limiter that allows everything. It must be chosen
	// deliberately — an unset or unrecognized provider is an error, because a
	// limiter that silently stops limiting is indistinguishable from one that is
	// simply never hit.
	ProviderNoop = "noop"
	// ProviderRedis selects the redis-backed limiter.
	ProviderRedis = "redis"

	defaultRequestsPerSec = 10.0
	defaultBurstSize      = 20
)

// providers are every provider this package implements. The dispatch switch and
// ValidateWithContext both read it, so they cannot drift apart.
var providers = []string{ProviderMemory, ProviderNoop, ProviderRedis}

// Config configures rate limiting.
type Config struct {
	Provider       string         `env:"PROVIDER"         json:"provider,omitempty"          yaml:"provider,omitempty"`
	Redis          redisrl.Config `env:",init"            envPrefix:"REDIS_"                 json:"redis,omitzero"              yaml:"redis,omitempty"`
	RequestsPerSec float64        `env:"REQUESTS_PER_SEC" json:"requestsPerSecond,omitempty" yaml:"requestsPerSecond,omitempty"`
	BurstSize      int            `env:"BURST_SIZE"       json:"burstSize,omitempty"         yaml:"burstSize,omitempty"`
}

var _ validation.ValidatableWithContext = (*Config)(nil)

// EnsureDefaults sets default values for zero fields.
func (cfg *Config) EnsureDefaults() {
	if cfg.RequestsPerSec == 0 {
		cfg.RequestsPerSec = defaultRequestsPerSec
	}
	if cfg.BurstSize == 0 {
		cfg.BurstSize = defaultBurstSize
	}
}

// ValidateWithContext validates the config.
func (cfg *Config) ValidateWithContext(ctx context.Context) error {
	return validation.ValidateStructWithContext(ctx, cfg,
		validation.Field(&cfg.Provider, validation.Required, validation.By(func(any) error {
			if !slices.Contains(providers, cfgnorm.Provider(cfg.Provider)) {
				return errors.Wrapf(errors.ErrUnknownProvider, "rate limiter provider %q", cfg.Provider)
			}

			return nil
		})),
		validation.Field(&cfg.RequestsPerSec, validation.Min(0.0)),
		validation.Field(&cfg.BurstSize, validation.Min(0)),
	)
}

// NewRateLimiter returns a RateLimiter from config.
//
// Defaults are applied before validation, so an unset RequestsPerSec is the
// documented default rather than a validation failure; a negative one is still
// rejected, because a negative rate rejects every request.
func NewRateLimiter(
	ctx context.Context,
	cfg *Config,
	opts ...Option,
) (ratelimiting.RateLimiter, error) {
	o := newOptions(opts)
	logger, tracerProvider, metricsProvider := o.logger, o.tracerProvider, o.metricsProvider

	if cfg == nil {
		return nil, errors.ErrNilInputParameter
	}

	cfg.EnsureDefaults()

	// Checked before the rest of the config so an unrecognized provider reports
	// ErrUnknownProvider rather than a downstream consequence of it.
	provider, err := cfgnorm.SelectProvider(cfg.Provider, providers, "rate limiter provider")
	if err != nil {
		return nil, err
	}

	if err = cfg.ValidateWithContext(ctx); err != nil {
		return nil, errors.Wrap(err, "validating rate limiter config")
	}

	switch provider {
	case ProviderNoop:
		return noop.NewRateLimiter(), nil
	case ProviderMemory:
		return ratelimiting.NewInMemoryRateLimiter(cfg.RequestsPerSec, cfg.BurstSize,
			ratelimiting.WithLogger(logger),
			ratelimiting.WithTracerProvider(tracerProvider),
			ratelimiting.WithMetricsProvider(metricsProvider))
	case ProviderRedis:
		return redisrl.NewRedisRateLimiter(ctx, cfg.Redis, cfg.RequestsPerSec, cfg.BurstSize,
			redisrl.WithLogger(logger),
			redisrl.WithTracerProvider(tracerProvider),
			redisrl.WithMetricsProvider(metricsProvider))
	default:
		return nil, errors.Wrapf(errors.ErrUnknownProvider, "rate limiter provider %q", cfg.Provider)
	}
}
