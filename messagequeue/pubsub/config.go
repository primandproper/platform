package pubsub

import (
	"context"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

// Config configures a PubSub-backed pubSubConsumer.
type Config struct {
	ProjectID string `env:"PROJECT_ID" json:"projectID,omitempty" yaml:"projectID,omitempty"`
}

var _ validation.ValidatableWithContext = (*Config)(nil)

// ValidateWithContext validates a Config struct.
//
// The project ID is required because the Pub/Sub client cannot be built without
// one — it was already the difference between a working consumer and a
// construction error, and saying so here is what moves the report to startup.
func (cfg *Config) ValidateWithContext(ctx context.Context) error {
	return validation.ValidateStructWithContext(ctx, cfg,
		validation.Field(&cfg.ProjectID, validation.Required),
	)
}
