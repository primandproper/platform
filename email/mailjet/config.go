package mailjet

import (
	"context"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

type (
	// Config configures Mailjet to send email.
	Config struct {
		APIKey    string `env:"API_KEY"    json:"publicKey,omitempty" yaml:"publicKey,omitempty"`
		SecretKey string `env:"SECRET_KEY" json:"secretKey,omitempty" yaml:"secretKey,omitempty"`
	}
)

var _ validation.ValidatableWithContext = (*Config)(nil)

// ValidateWithContext validates a Config struct.
func (s *Config) ValidateWithContext(ctx context.Context) error {
	return validation.ValidateStructWithContext(ctx, s,
		validation.Field(&s.APIKey, validation.Required),
		validation.Field(&s.SecretKey, validation.Required),
	)
}
