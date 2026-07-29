package memory

import (
	"github.com/primandproper/platform-go/v8/observability/logging"
	"github.com/primandproper/platform-go/v8/observability/metrics"
	"github.com/primandproper/platform-go/v8/observability/tracing"
)

// WithLogger attaches a logger. An absent logger logs nowhere.
func WithLogger[T any](logger logging.Logger) Option[T] {
	return func(i *inMemoryCacheImpl[T]) { i.logger = logger }
}

// WithTracerProvider attaches a tracer provider, enabling spans on every cache
// operation. An absent tracer provider traces nowhere.
func WithTracerProvider[T any](tracerProvider tracing.TracerProvider) Option[T] {
	return func(i *inMemoryCacheImpl[T]) { i.tracerProvider = tracerProvider }
}

// WithMetricsProvider attaches a metrics provider for the cache's hit, miss,
// set, delete, and eviction counters and its latency histogram. An absent
// provider records nothing.
func WithMetricsProvider[T any](metricsProvider metrics.Provider) Option[T] {
	return func(i *inMemoryCacheImpl[T]) { i.metricsProvider = metricsProvider }
}
