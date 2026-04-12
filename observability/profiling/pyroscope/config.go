package pyroscope

import (
	"context"
	"time"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

// Config holds Pyroscope-specific profiling configuration.
type Config struct {
	Tags               map[string]string `env:"-"                    json:"tags,omitempty"`
	ServerAddress      string            `env:"SERVER_ADDRESS"       json:"serverAddress"`
	BasicAuthUser      string            `env:"BASIC_AUTH_USER"      json:"basicAuthUser,omitempty"`
	BasicAuthPassword  string            `env:"BASIC_AUTH_PASSWORD"  json:"basicAuthPassword,omitempty"`
	UploadRate         time.Duration     `env:"UPLOAD_RATE"          json:"uploadRate"`
	Insecure           bool              `env:"INSECURE"             json:"insecure"`
	EnableMutexProfile bool              `env:"ENABLE_MUTEX_PROFILE" json:"enableMutexProfile"`
	EnableBlockProfile bool              `env:"ENABLE_BLOCK_PROFILE" json:"enableBlockProfile"`
}

var _ validation.ValidatableWithContext = (*Config)(nil)

// ValidateWithContext validates the config struct.
func (c *Config) ValidateWithContext(ctx context.Context) error {
	return validation.ValidateStructWithContext(ctx, c,
		validation.Field(&c.ServerAddress, validation.Required),
		validation.Field(&c.UploadRate, validation.Required),
	)
}
