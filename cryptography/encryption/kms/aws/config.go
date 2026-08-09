package aws

import (
	"context"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

// Config configures the AWS KMS key wrapper.
type Config struct {
	// Region is the AWS region the key lives in.
	Region string `env:"REGION" json:"region,omitempty" yaml:"region,omitempty"`
	// KeyID identifies the customer master key that wraps. A key ID, a key
	// ARN, an alias name, or an alias ARN are all accepted, because AWS
	// accepts all four and rejecting three of them here would only mean
	// callers reformatting a value that already works.
	//
	// An alias is usually the right choice: it survives the key being replaced.
	KeyID string `env:"KEY_ID" json:"keyID,omitempty" yaml:"keyID,omitempty"`
}

var _ validation.ValidatableWithContext = (*Config)(nil)

// ValidateWithContext validates a Config struct.
func (cfg *Config) ValidateWithContext(ctx context.Context) error {
	return validation.ValidateStructWithContext(ctx, cfg,
		validation.Field(&cfg.Region, validation.Required),
		validation.Field(&cfg.KeyID, validation.Required),
	)
}
