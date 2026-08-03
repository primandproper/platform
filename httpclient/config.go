package httpclient

import (
	"context"
	"time"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

const (
	defaultTimeout             = 10 * time.Second
	defaultMaxIdleConns        = 100
	defaultMaxIdleConnsPerHost = 100
)

// Config configures an HTTP client.
type Config struct {
	Timeout             time.Duration `env:"TIMEOUT"                 json:"timeout,omitempty"             yaml:"timeout,omitempty"`
	MaxIdleConns        int           `env:"MAX_IDLE_CONNS"          json:"maxIdleConns,omitempty"        yaml:"maxIdleConns,omitempty"`
	MaxIdleConnsPerHost int           `env:"MAX_IDLE_CONNS_PER_HOST" json:"maxIdleConnsPerHost,omitempty" yaml:"maxIdleConnsPerHost,omitempty"`
	EnableTracing       bool          `env:"ENABLE_TRACING"          json:"enableTracing,omitempty"       yaml:"enableTracing,omitempty"`
}

var _ validation.ValidatableWithContext = (*Config)(nil)

// EnsureDefaults sets default values for zero fields.
func (cfg *Config) EnsureDefaults() {
	if cfg.Timeout == 0 {
		cfg.Timeout = defaultTimeout
	}
	if cfg.MaxIdleConns == 0 {
		cfg.MaxIdleConns = defaultMaxIdleConns
	}
	if cfg.MaxIdleConnsPerHost == 0 {
		cfg.MaxIdleConnsPerHost = defaultMaxIdleConnsPerHost
	}
}

// Options expresses the config as the equivalent list of Options, which is how a
// Config reaches NewHTTPClient:
//
//	client := httpclient.NewHTTPClient(cfg.Options()...)
//
// Callers can append further Options to override individual settings. Zero-valued
// numeric fields yield Options that leave the package defaults in place, matching
// EnsureDefaults; EnableTracing is applied as given. A nil Config yields no Options.
func (cfg *Config) Options() []Option {
	if cfg == nil {
		return nil
	}

	return []Option{
		WithTimeout(cfg.Timeout),
		WithMaxIdleConns(cfg.MaxIdleConns),
		WithMaxIdleConnsPerHost(cfg.MaxIdleConnsPerHost),
		WithTracing(cfg.EnableTracing),
	}
}

// ValidateWithContext validates the config.
func (cfg *Config) ValidateWithContext(ctx context.Context) error {
	return validation.ValidateStructWithContext(ctx, cfg,
		validation.Field(&cfg.Timeout, validation.Required, validation.Min(time.Millisecond)),
		validation.Field(&cfg.MaxIdleConns, validation.Required, validation.Min(1)),
		validation.Field(&cfg.MaxIdleConnsPerHost, validation.Required, validation.Min(1)),
	)
}
