package database

import (
	"github.com/primandproper/platform-go/v9/observability/logging"
	"github.com/primandproper/platform-go/v9/observability/metrics"
	"github.com/primandproper/platform-go/v9/observability/tracing"
)

// Option configures a Resolver.
type Option func(*Resolver)

// WithLogger attaches a logger.
func WithLogger(logger logging.Logger) Option {
	return func(r *Resolver) {
		r.logger = logger
	}
}

// WithTracerProvider attaches a tracer provider, so a policy resolution shows
// up as a child of the span that triggered it.
func WithTracerProvider(tracerProvider tracing.TracerProvider) Option {
	return func(r *Resolver) {
		r.tracerProvider = tracerProvider
	}
}

// WithMetricsProvider attaches a metrics provider.
func WithMetricsProvider(metricsProvider metrics.Provider) Option {
	return func(r *Resolver) {
		r.metricsProvider = metricsProvider
	}
}
