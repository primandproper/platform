package postgres

import (
	"context"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

// Config configures a Postgres-backed distributed locker. Namespace is mixed into
// the lock-id hash so independent services that share a Postgres cluster do not
// collide on the same advisory-lock id space.
type Config struct {
	Namespace int32 `env:"NAMESPACE" envDefault:"0" json:"namespace"`
}

var _ validation.ValidatableWithContext = (*Config)(nil)

// ValidateWithContext validates a Config struct. Namespace has no upper bound;
// any int32 is acceptable.
func (cfg *Config) ValidateWithContext(_ context.Context) error {
	return validation.Validate(cfg)
}
