package ollama

import (
	"context"
	"time"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

var _ validation.ValidatableWithContext = (*Config)(nil)

// Config configures the Ollama embeddings provider.
type Config struct {
	BaseURL      string        `env:"BASE_URL"      json:"baseURL,omitempty"`
	DefaultModel string        `env:"DEFAULT_MODEL" json:"defaultModel,omitempty"`
	Timeout      time.Duration `env:"TIMEOUT"       json:"timeout"`
}

// ValidateWithContext validates the config.
func (c *Config) ValidateWithContext(ctx context.Context) error {
	return validation.ValidateStructWithContext(ctx, c)
}
