package featureflagscfg

import (
	"context"
	"net/http"
	"strings"

	"github.com/primandproper/platform-go/v9/circuitbreaking"
	circuitbreakingcfg "github.com/primandproper/platform-go/v9/circuitbreaking/config"
	"github.com/primandproper/platform-go/v9/errors"
	"github.com/primandproper/platform-go/v9/featureflags"
	"github.com/primandproper/platform-go/v9/featureflags/launchdarkly"
	"github.com/primandproper/platform-go/v9/featureflags/noop"
	"github.com/primandproper/platform-go/v9/featureflags/posthog"
	"github.com/primandproper/platform-go/v9/observability/logging"
	"github.com/primandproper/platform-go/v9/observability/metrics"
	"github.com/primandproper/platform-go/v9/observability/tracing"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

const (
	// ProviderLaunchDarkly is used to indicate the LaunchDarkly provider.
	ProviderLaunchDarkly = "launchdarkly"
	// ProviderPostHog is used to indicate the PostHog provider.
	ProviderPostHog = "posthog"
	// ProviderNoop reports every flag as its default. It must be selected
	// deliberately — an unset or typo'd provider is an error, because a manager
	// that silently answers "off" for everything looks like a quiet rollout.
	ProviderNoop = "noop"
)

type (
	// Config configures our feature flag manager.
	Config struct {
		LaunchDarkly   *launchdarkly.Config      `env:",init"    envPrefix:"LAUNCH_DARKLY_"    json:"launchDarkly"         yaml:"launchDarkly"`
		PostHog        *posthog.Config           `env:",init"    envPrefix:"POSTHOG_"          json:"posthog"              yaml:"posthog"`
		Provider       string                    `env:"PROVIDER" json:"provider"               yaml:"provider"`
		CircuitBreaker circuitbreakingcfg.Config `env:",init"    envPrefix:"CIRCUIT_BREAKING_" json:"circuitBreakerConfig" yaml:"circuitBreakerConfig"`
	}
)

var _ validation.ValidatableWithContext = (*Config)(nil)

// EnsureDefaults sets sensible defaults for zero-valued fields.
func (cfg *Config) EnsureDefaults() {
	cfg.CircuitBreaker.EnsureDefaults()
}

// ValidateWithContext validates the config.
func (c *Config) ValidateWithContext(ctx context.Context) error {
	return validation.ValidateStructWithContext(ctx, c,
		validation.Field(&c.Provider, validation.Required, validation.In(ProviderLaunchDarkly, ProviderPostHog, ProviderNoop)),
		validation.Field(&c.LaunchDarkly, validation.When(c.Provider == ProviderLaunchDarkly, validation.Required)),
		validation.Field(&c.PostHog, validation.When(c.Provider == ProviderPostHog, validation.Required)),
	)
}

func (c *Config) NewFeatureFlagManager(ctx context.Context, logger logging.Logger, tracerProvider tracing.TracerProvider, metricsProvider metrics.Provider, httpClient *http.Client, circuitBreaker circuitbreaking.CircuitBreaker) (featureflags.FeatureFlagManager, error) {
	c.EnsureDefaults()

	if err := c.ValidateWithContext(ctx); err != nil {
		return nil, errors.Wrap(err, "validating feature flag config")
	}

	switch strings.TrimSpace(strings.ToLower(c.Provider)) {
	case ProviderLaunchDarkly:
		return launchdarkly.NewFeatureFlagManager(c.LaunchDarkly, httpClient, circuitBreaker, launchdarkly.WithLogger(logger), launchdarkly.WithTracerProvider(tracerProvider), launchdarkly.WithMetricsProvider(metricsProvider))
	case ProviderPostHog:
		return posthog.NewFeatureFlagManager(c.PostHog, circuitBreaker, posthog.WithLogger(logger), posthog.WithTracerProvider(tracerProvider), posthog.WithMetricsProvider(metricsProvider))
	case ProviderNoop:
		return noop.NewFeatureFlagManager(), nil
	default:
		return nil, errors.Wrapf(errors.ErrUnknownProvider, "feature flag provider %q", c.Provider)
	}
}
