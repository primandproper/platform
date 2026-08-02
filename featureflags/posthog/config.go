package posthog

import (
	"context"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

type (
	Config struct {
		ProjectAPIKey  string `env:"PROJECT_API_KEY"  json:"projectAPIKey"  yaml:"projectAPIKey"`
		PersonalAPIKey string `env:"PERSONAL_API_KEY" json:"personalAPIKey" yaml:"personalAPIKey"`
		// Endpoint is the PostHog host. Leave empty for PostHog US Cloud (the SDK
		// default); set it for EU Cloud (https://eu.posthog.com) or self-hosted.
		Endpoint string `env:"ENDPOINT" json:"endpoint" yaml:"endpoint"`
	}
)

var _ validation.ValidatableWithContext = (*Config)(nil)

// ValidateWithContext validates the Config.
//
// ProjectAPIKey is required; PersonalAPIKey and Endpoint are not, since the
// former is only needed for the local-evaluation API and the latter defaults to
// PostHog US Cloud.
func (cfg *Config) ValidateWithContext(ctx context.Context) error {
	return validation.ValidateStructWithContext(ctx, cfg,
		validation.Field(&cfg.ProjectAPIKey, validation.Required),
	)
}
