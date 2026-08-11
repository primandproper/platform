package posthog

import (
	"context"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

type (
	Config struct {
		ProjectAPIKey  string `env:"PROJECT_API_KEY"  json:"projectAPIKey,omitempty"  yaml:"projectAPIKey,omitempty"`
		PersonalAPIKey string `env:"PERSONAL_API_KEY" json:"personalAPIKey,omitempty" yaml:"personalAPIKey,omitempty"`
		// Endpoint is the PostHog host. Leave empty for PostHog US Cloud (the SDK
		// default); set it for EU Cloud (https://eu.posthog.com) or self-hosted.
		Endpoint string `env:"ENDPOINT" json:"endpoint,omitempty" yaml:"endpoint,omitempty"`
	}
)

var _ validation.ValidatableWithContext = (*Config)(nil)

// ValidateWithContext validates the Config.
//
// Both keys are required, and Endpoint is not — it defaults to PostHog US
// Cloud. PersonalAPIKey used to be documented here as optional "since it is
// only needed for the local-evaluation API", which is true of the SDK in
// general and false of this package: the thing being built is a feature flag
// manager, and the SDK answers every flag evaluation with "specifying a
// PersonalApiKey is required for using feature flags" without one. It was
// already required by NewFeatureFlagManager; the disagreement was that a config
// naming only the project key validated clean and then failed to build.
func (cfg *Config) ValidateWithContext(ctx context.Context) error {
	return validation.ValidateStructWithContext(ctx, cfg,
		validation.Field(&cfg.ProjectAPIKey, validation.Required),
		validation.Field(&cfg.PersonalAPIKey, validation.Required),
	)
}
