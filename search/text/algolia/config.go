package algolia

import (
	"context"
	"time"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

type Config struct {
	AppID   string        `env:"APP_ID"  json:"appID,omitempty"       yaml:"appID,omitempty"`
	APIKey  string        `env:"API_KEY" json:"writeAPIKey,omitempty" yaml:"writeAPIKey,omitempty"`
	Timeout time.Duration `env:"TIMEOUT" json:"timeout,omitempty"     yaml:"timeout,omitempty"`
}

var _ validation.ValidatableWithContext = (*Config)(nil)

// ValidateWithContext validates the Config.
//
// Both credentials are required: Algolia's client accepts empty ones and fails
// per request instead, which turns a configuration mistake into a search outage
// discovered in production rather than at startup.
func (cfg *Config) ValidateWithContext(ctx context.Context) error {
	return validation.ValidateStructWithContext(ctx, cfg,
		validation.Field(&cfg.AppID, validation.Required),
		validation.Field(&cfg.APIKey, validation.Required),
	)
}
