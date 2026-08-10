package sqs

import (
	"context"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

// Config configures a SQS-backed consumer.
type Config struct {
	// QueueAddress overrides the AWS endpoint the SDK would otherwise resolve,
	// which is what points this at localstack. Leaving it empty is the ordinary
	// case: credentials and region come from the ambient AWS configuration.
	QueueAddress string `env:"QUEUE_ADDRESS" json:"queueAddress,omitempty" yaml:"queueAddress,omitempty"`
}

var _ validation.ValidatableWithContext = (*Config)(nil)

// ValidateWithContext validates a Config struct.
//
// It requires nothing, and exists so that the config subpackage can validate
// every provider's block the same way rather than skipping the one that had no
// method. The emptiness is the statement: an SQS config that names nothing is a
// deployment using the real AWS endpoint, which is the common one.
func (cfg *Config) ValidateWithContext(ctx context.Context) error {
	return validation.ValidateStructWithContext(ctx, cfg)
}
