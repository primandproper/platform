package ratelimiting

import (
	"github.com/primandproper/platform-go/v9/observability/metrics"
)

// Option configures the in-memory rate limiter this package constructs. The
// zero configuration works: an absent metrics provider records nothing.
type Option func(*options)

type options struct {
	metricsProvider metrics.Provider
}

func newOptions(opts []Option) *options {
	cfg := &options{}
	for _, opt := range opts {
		if opt != nil {
			opt(cfg)
		}
	}

	return cfg
}

// WithMetricsProvider attaches a metrics provider for the limiter's allowed
// and rejected counters.
func WithMetricsProvider(metricsProvider metrics.Provider) Option {
	return func(o *options) { o.metricsProvider = metricsProvider }
}
