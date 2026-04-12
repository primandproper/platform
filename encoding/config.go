package encoding

import (
	"context"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

// Config configures input/output encoding for the service.
type Config struct {
	_ struct{} `json:"-"`

	ContentType string `env:"CONTENT_TYPE" json:"contentType"`
}

var _ validation.ValidatableWithContext = (*Config)(nil)

// ValidateWithContext validates a Config struct.
func (cfg *Config) ValidateWithContext(ctx context.Context) error {
	return validation.ValidateStructWithContext(ctx, cfg,
		validation.Field(&cfg.ContentType, validation.Required),
	)
}
