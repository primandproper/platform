package writes

import (
	"github.com/primandproper/platform-go/v12/observability"
	"github.com/primandproper/platform-go/v12/observability/logging"
	"github.com/primandproper/platform-go/v12/observability/metrics"
	"github.com/primandproper/platform-go/v12/observability/tracing"
)

// Option configures a Writer at construction.
type Option func(*options)

// options accumulates what the options set.
type options struct {
	logger          logging.Logger
	tracerProvider  tracing.Provider
	metricsProvider metrics.Provider

	hooks []Hook
}

// WithHook registers a hook to run inside the transaction of every write.
//
// This is where an application's conventions attach. Audit entries, outbox
// events, search-index stamps, cache invalidations: none of them belong to this
// package, and all of them belong in the transaction that wrote the row. A
// Writer with no hooks is a transaction wrapper and nothing else, which is a
// perfectly good thing for it to be — the call sites are already the right shape
// for the day one is added.
//
// Hooks run in registration order, and the first error stops the rest and rolls
// the transaction back.
func WithHook(hook Hook) Option {
	return func(o *options) {
		if hook != nil {
			o.hooks = append(o.hooks, hook)
		}
	}
}

// WithLogger attaches a logger. An absent logger logs nowhere.
func WithLogger(logger logging.Logger) Option {
	return func(o *options) { o.logger = logger }
}

// WithTracerProvider attaches a tracer provider, so a write's transaction shows
// up as a child of the span that owns the request. An absent one traces nowhere.
func WithTracerProvider(tracerProvider tracing.Provider) Option {
	return func(o *options) { o.tracerProvider = tracerProvider }
}

// WithMetricsProvider attaches a metrics provider. An absent one records
// nowhere.
//
// What it records is the trio every component in this module records — attempts,
// failures, latency — under one "database_writes" prefix, with the stage a
// failure came from as an attribute. See metrics.OperationSet.
func WithMetricsProvider(metricsProvider metrics.Provider) Option {
	return func(o *options) { o.metricsProvider = metricsProvider }
}

// WithPillars supplies the logger, tracer provider, and metrics provider at
// once.
//
// Options apply in order, so a caller wanting all of it but one names the
// pillars and then overrides: WithPillars(p) followed by
// WithMetricsProvider(nil) leaves this one writer unmetered.
func WithPillars(pillars *observability.Pillars) Option {
	return func(o *options) {
		o.logger, o.tracerProvider, o.metricsProvider = pillars.Deps()
	}
}
