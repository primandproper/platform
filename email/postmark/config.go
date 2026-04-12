package postmark

import (
	"context"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

type (
	// Config configures Postmark to send email.
	Config struct {
		ServerToken string `env:"SERVER_TOKEN" json:"serverToken"`
		// BaseURL overrides the API base URL (e.g. for testing with httptest).
		BaseURL string `env:"BASE_URL" json:"baseURL"`
	}
)

var _ validation.ValidatableWithContext = (*Config)(nil)

// ValidateWithContext validates a Config struct.
func (s *Config) ValidateWithContext(ctx context.Context) error {
	return validation.ValidateStructWithContext(ctx, s,
		validation.Field(&s.ServerToken, validation.Required),
	)
}
