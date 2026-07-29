package memory

import (
	"github.com/primandproper/platform-go/v8/observability/logging"
	"github.com/primandproper/platform-go/v8/observability/metrics"
	"github.com/primandproper/platform-go/v8/observability/tracing"
)

// WithLogger attaches a logger. An absent logger logs nowhere.
func WithLogger(logger logging.Logger) Option {
	return func(o *options) { o.logger = logger }
}

// WithTracerProvider attaches a tracer provider, enabling spans on every cache
// operation. An absent tracer provider traces nowhere.
func WithTracerProvider(tracerProvider tracing.TracerProvider) Option {
	return func(o *options) { o.tracerProvider = tracerProvider }
}

// WithMetricsProvider attaches a metrics provider for the cache's hit, miss,
// set, delete, and eviction counters and its latency histogram. An absent
// provider records nothing.
func WithMetricsProvider(metricsProvider metrics.Provider) Option {
	return func(o *options) { o.metricsProvider = metricsProvider }
}
