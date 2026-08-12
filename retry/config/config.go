// Package retrycfg builds a retry.Policy from configuration — exponential
// backoff, which an unset provider selects, or noop, which has to be named.
//
// Its Config is embedded in the configs of the packages that retry (outbox,
// saga, metering, dataprivacy, jobs, webhooks) as well as read from the
// environment directly, which is why its zero value is meaningful and why
// EnsureDefaults clamps rather than merely zero-checks: the constructor returns
// no error a nonsensical multiplier could travel out through.
//
// It also exports the schedule itself. DelayFor and ScheduledDelayFor exist for
// callers that cannot retry by sleeping — a worker persisting "try again at T"
// into a row has to survive the process — so the backoff a policy would have
// waited and the backoff a row is scheduled for are computed once rather than
// twice.
package retrycfg

import (
	"context"
	"time"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

const (
	defaultMaxAttempts  = 3
	defaultInitialDelay = 100 * time.Millisecond
	defaultMaxDelay     = 5 * time.Second
	defaultMultiplier   = 2.0
)

// Config configures retry behavior.
//
// It is embedded in the configs of packages that retry (outbox, saga, metering,
// dataprivacy, jobs, webhooks) as well as read from the environment directly,
// which is why the zero value is meaningful: EnsureDefaults fills it in.
type Config struct {
	Provider     string        `env:"PROVIDER"      json:"provider,omitempty"     yaml:"provider,omitempty"`
	MaxAttempts  uint          `env:"MAX_ATTEMPTS"  json:"maxAttempts,omitempty"  yaml:"maxAttempts,omitempty"`
	InitialDelay time.Duration `env:"INITIAL_DELAY" json:"initialDelay,omitempty" yaml:"initialDelay,omitempty"`
	MaxDelay     time.Duration `env:"MAX_DELAY"     json:"maxDelay,omitempty"     yaml:"maxDelay,omitempty"`
	Multiplier   float64       `env:"MULTIPLIER"    json:"multiplier,omitempty"   yaml:"multiplier,omitempty"`
	UseJitter    bool          `env:"USE_JITTER"    json:"useJitter,omitempty"    yaml:"useJitter,omitempty"`
}

var _ validation.ValidatableWithContext = (*Config)(nil)

// EnsureDefaults sets default values for zero (and invalid) fields. It clamps
// rather than merely zero-checking so a nonsensical config — a negative delay or
// a Multiplier below 1 that would shrink the backoff — can't produce a
// pathological policy, since the constructor returns no error to reject it.
func (cfg *Config) EnsureDefaults() {
	if cfg.MaxAttempts == 0 {
		cfg.MaxAttempts = defaultMaxAttempts
	}

	if cfg.InitialDelay <= 0 {
		cfg.InitialDelay = defaultInitialDelay
	}

	if cfg.MaxDelay <= 0 {
		cfg.MaxDelay = defaultMaxDelay
	}

	if cfg.Multiplier < 1 {
		cfg.Multiplier = defaultMultiplier
	}
}

// ValidateWithContext validates the config.
func (cfg *Config) ValidateWithContext(ctx context.Context) error {
	return validation.ValidateStructWithContext(ctx, cfg,
		validation.Field(&cfg.Provider, validation.In(providers...)),
		validation.Field(&cfg.MaxAttempts, validation.Required, validation.Min(uint(1))),
		validation.Field(&cfg.InitialDelay, validation.Required, validation.Min(time.Millisecond)),
		validation.Field(&cfg.MaxDelay, validation.Required, validation.Min(time.Millisecond)),
		validation.Field(&cfg.Multiplier, validation.Min(1.0)),
	)
}
