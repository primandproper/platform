package elasticsearch

import (
	"context"
	"time"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

type Config struct {
	Address               string        `env:"ADDRESS"                 json:"address,omitempty"               yaml:"address,omitempty"`
	Username              string        `env:"USERNAME"                json:"username,omitempty"              yaml:"username,omitempty"`
	Password              string        `env:"PASSWORD"                json:"password,omitempty"              yaml:"password,omitempty"`
	CACert                []byte        `env:"CA_CERT"                 json:"caCert,omitempty"                yaml:"caCert,omitempty"`
	IndexOperationTimeout time.Duration `env:"INDEX_OPERATION_TIMEOUT" json:"indexOperationTimeout,omitempty" yaml:"indexOperationTimeout,omitempty"`
}

var _ validation.ValidatableWithContext = (*Config)(nil)

// ValidateWithContext validates the Config.
//
// Address is required. Username and Password are not: a cluster reachable
// without authentication is a normal local and test setup.
func (cfg *Config) ValidateWithContext(ctx context.Context) error {
	return validation.ValidateStructWithContext(ctx, cfg,
		validation.Field(&cfg.Address, validation.Required),
	)
}
