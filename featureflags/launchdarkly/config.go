package launchdarkly

import (
	"time"
)

type (
	Config struct {
		SDKKey      string        `env:"SDK_KEY"      json:"sdkKey"      yaml:"sdkKey"`
		InitTimeout time.Duration `env:"INIT_TIMEOUT" json:"initTimeout" yaml:"initTimeout"`
	}
)
