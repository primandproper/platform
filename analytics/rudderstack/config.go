package rudderstack

import (
	"context"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

type Config struct {
	APIKey       string `env:"API_KEY"        json:"apiKey"`
	DataPlaneURL string `env:"DATA_PLANE_URL" json:"dataPlaneURL"`
}

var _ validation.ValidatableWithContext = (*Config)(nil)

// ValidateWithContext validates a Config struct.
func (cfg *Config) ValidateWithContext(ctx context.Context) error {
	return validation.ValidateStructWithContext(ctx, cfg,
		validation.Field(&cfg.APIKey, validation.Required),
		validation.Field(&cfg.DataPlaneURL, validation.Required),
	)
}
