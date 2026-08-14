package webauthn

import (
	"github.com/primandproper/platform-go/v10/observability/logging"
	"github.com/primandproper/platform-go/v10/observability/metrics"
	"github.com/primandproper/platform-go/v10/observability/tracing"
)

// Option configures the RelyingParty this package constructs. The zero
// configuration works: an absent logger logs nowhere, an absent tracer provider
// traces nowhere, and an absent metrics provider records nothing.
type Option func(*options)

type options struct {
	logger          logging.Logger
	tracerProvider  tracing.Provider
	metricsProvider metrics.Provider
}

// newOptions applies opts, ignoring nil entries.
func newOptions(opts []Option) *options {
	o := &options{}
	for _, opt := range opts {
		if opt != nil {
			opt(o)
		}
	}

	return o
}

// WithLogger attaches a logger.
//
// Worth setting. A ceremony that fails verification is a security-relevant
// event — a challenge answered from an origin that is not configured, a
// credential presented by a user who does not own it — and without a logger the
// only trace it leaves is whatever the caller does with the returned error.
func WithLogger(logger logging.Logger) Option {
	return func(o *options) { o.logger = logger }
}

// WithTracerProvider attaches a tracer provider, enabling spans on every
// ceremony step.
func WithTracerProvider(tracerProvider tracing.Provider) Option {
	return func(o *options) { o.tracerProvider = tracerProvider }
}

// WithMetricsProvider attaches a metrics provider for the ceremony counters and
// latency histogram. An absent provider records nothing.
func WithMetricsProvider(metricsProvider metrics.Provider) Option {
	return func(o *options) { o.metricsProvider = metricsProvider }
}
