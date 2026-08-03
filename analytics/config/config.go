package analyticscfg

import (
	"context"
	"strings"

	"github.com/primandproper/platform-go/v9/analytics"
	"github.com/primandproper/platform-go/v9/analytics/noop"
	"github.com/primandproper/platform-go/v9/analytics/posthog"
	"github.com/primandproper/platform-go/v9/analytics/segment"
	circuitbreakingcfg "github.com/primandproper/platform-go/v9/circuitbreaking/config"
	"github.com/primandproper/platform-go/v9/errors"

	validation "github.com/go-ozzo/ozzo-validation/v4"
	posthogsdk "github.com/posthog/posthog-go"
)

const (
	// ProviderSegment represents Segment.
	ProviderSegment = "segment"
	// ProviderPostHog represents PostHog.
	ProviderPostHog = "posthog"
	// ProviderNoop discards every event. It must be selected deliberately — an
	// unset or typo'd provider is an error, because analytics that silently stop
	// being recorded are only noticed when someone asks a question of the data
	// months later.
	ProviderNoop = "noop"
)

type (
	// SourceConfig is the per-source analytics config (provider + credentials). Used for proxy sources; no ProxySources to avoid recursion.
	SourceConfig struct {
		Segment        *segment.Config           `env:",init"                  envPrefix:"SEGMENT_"           json:"segment,omitempty"        yaml:"segment,omitempty"`
		Posthog        *posthog.Config           `env:",init"                  envPrefix:"POSTHOG_"           json:"posthog,omitempty"        yaml:"posthog,omitempty"`
		Provider       string                    `env:"PROVIDER"               json:"provider,omitempty"      yaml:"provider,omitempty"`
		CircuitBreaker circuitbreakingcfg.Config `envPrefix:"CIRCUIT_BREAKER_" json:"circuitBreaker,omitzero" yaml:"circuitBreaker,omitempty"`
	}

	// ProxySourcesConfig holds per-source analytics config for the analytics
	// proxy gRPC service, keyed by source name.
	//
	// It is a map rather than a struct with a field per source: the set of
	// sources belongs to the application, not to this module, and every source
	// an application adds would otherwise be a breaking change to an exported
	// struct here. Source names are free-form; "ios" and "web" are conventional,
	// not special.
	//
	// Environment parsing populates it from PROXY_SOURCES_<NAME>_* keys.
	ProxySourcesConfig map[string]*SourceConfig

	// Config is the configuration structure.
	Config struct {
		ProxySources ProxySourcesConfig `envPrefix:"PROXY_SOURCES_" json:"proxySources,omitempty" yaml:"proxySources,omitempty"`
		SourceConfig
	}
)

var _ validation.ValidatableWithContext = (*Config)(nil)

// EnsureDefaults sets sensible defaults for zero-valued fields.
func (cfg *SourceConfig) EnsureDefaults() {
	cfg.CircuitBreaker.EnsureDefaults()
}

// EnsureDefaults sets sensible defaults for zero-valued fields.
func (cfg *Config) EnsureDefaults() {
	cfg.SourceConfig.EnsureDefaults()

	for _, src := range cfg.ProxySources {
		if src != nil {
			src.EnsureDefaults()
		}
	}
}

// ToMap returns the configured sources keyed by name, skipping nil entries. It
// is what the multisource reporter consumes.
func (p ProxySourcesConfig) ToMap() map[string]*SourceConfig {
	m := make(map[string]*SourceConfig, len(p))
	for name, src := range p {
		if src != nil {
			m[name] = src
		}
	}

	return m
}

// ValidateWithContext validates a SourceConfig: the provider must be known and the
// matching credentials block present, so a proxy source with no provider/key can't
// pass validation and silently degrade to a noop at runtime.
func (cfg *SourceConfig) ValidateWithContext(ctx context.Context) error {
	return validation.ValidateStructWithContext(ctx, cfg,
		validation.Field(&cfg.Provider, validation.Required, validation.In(ProviderSegment, ProviderPostHog, ProviderNoop)),
		validation.Field(&cfg.Segment, validation.When(cfg.Provider == ProviderSegment, validation.Required)),
		validation.Field(&cfg.Posthog, validation.When(cfg.Provider == ProviderPostHog, validation.Required)),
	)
}

// ValidateWithContext validates a Config struct.
func (cfg *Config) ValidateWithContext(ctx context.Context) error {
	// The root source goes through the same rules as every proxy source rather than
	// a restatement of them, which is what let the two drift: this one accepted an
	// empty provider and fell through to a noop, while the proxy sources required
	// one to be named.
	if err := cfg.SourceConfig.ValidateWithContext(ctx); err != nil {
		return err
	}

	// Each configured proxy source must itself be valid.
	for name, src := range cfg.ProxySources.ToMap() {
		if err := src.ValidateWithContext(ctx); err != nil {
			return errors.Wrapf(err, "validating %q proxy source", name)
		}
	}

	return nil
}

// NewCollector provides a collector.
func (cfg *SourceConfig) NewCollector(
	ctx context.Context,
	opts ...Option,
) (analytics.EventReporter, error) {
	o := newOptions(opts)
	logger, tracerProvider, metricsProvider := o.logger, o.tracerProvider, o.metricsProvider

	cb, err := cfg.CircuitBreaker.NewCircuitBreaker(ctx, circuitbreakingcfg.WithLogger(logger), circuitbreakingcfg.WithMetricsProvider(metricsProvider))
	if err != nil {
		return nil, errors.Wrap(err, "could not create analytics circuit breaker")
	}

	switch strings.ToLower(strings.TrimSpace(cfg.Provider)) {
	case ProviderSegment:
		if cfg.Segment == nil {
			return nil, errors.New("segment provider configured but segment config is nil")
		}
		return segment.NewEventReporter(cfg.Segment.APIToken, cb, segment.WithLogger(logger), segment.WithTracerProvider(tracerProvider), segment.WithMetricsProvider(metricsProvider))
	case ProviderPostHog:
		if cfg.Posthog == nil {
			return nil, errors.New("posthog provider configured but posthog config is nil")
		}
		var modifiers []func(*posthogsdk.Config)
		if cfg.Posthog.Endpoint != "" {
			endpoint := cfg.Posthog.Endpoint
			modifiers = append(modifiers, func(c *posthogsdk.Config) { c.Endpoint = endpoint })
		}
		return posthog.NewEventReporter(cfg.Posthog.APIKey, cb, posthog.WithLogger(logger), posthog.WithTracerProvider(tracerProvider), posthog.WithMetricsProvider(metricsProvider), posthog.WithConfigModifiers(modifiers...))
	case ProviderNoop:
		return noop.NewEventReporter(), nil
	default:
		return nil, errors.Wrapf(errors.ErrUnknownProvider, "analytics provider %q", cfg.Provider)
	}
}
