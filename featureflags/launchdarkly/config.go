package launchdarkly

import (
	"context"
	"time"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

type (
	Config struct {
		SDKKey      string        `env:"SDK_KEY"      json:"sdkKey,omitempty"      yaml:"sdkKey,omitempty"`
		InitTimeout time.Duration `env:"INIT_TIMEOUT" json:"initTimeout,omitempty" yaml:"initTimeout,omitempty"`
	}
)

var _ validation.ValidatableWithContext = (*Config)(nil)

// ValidateWithContext validates the Config.
//
// SDKKey is required: the client cannot reach LaunchDarkly without one, and an
// empty key would otherwise reach the SDK and report every flag as its default,
// which is indistinguishable from the flags genuinely being off.
func (cfg *Config) ValidateWithContext(ctx context.Context) error {
	return validation.ValidateStructWithContext(ctx, cfg,
		validation.Field(&cfg.SDKKey, validation.Required),
	)
}
