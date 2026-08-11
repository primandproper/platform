package retrycfg

import (
	"github.com/primandproper/platform-go/v10/clock"
	"github.com/primandproper/platform-go/v10/observability"
	"github.com/primandproper/platform-go/v10/observability/logging"
	"github.com/primandproper/platform-go/v10/observability/metrics"
	"github.com/primandproper/platform-go/v10/observability/tracing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// nameKey labels a policy's instruments and spans with what the loop is
// retrying.
const nameKey = "retry.name"

// Option configures a Policy built here. The zero configuration works: an
// absent logger logs nowhere, an absent tracer provider traces nowhere, an
// absent metrics provider records nothing, and an absent clock is the wall
// clock.
type Option func(*options)

type options struct {
	logger          logging.Logger
	tracerProvider  tracing.Provider
	metricsProvider metrics.Provider
	clock           clock.Clock
	name            string
}

func newOptions(opts []Option) *options {
	o := &options{clock: clock.NewClock()}
	for _, opt := range opts {
		if opt != nil {
			opt(o)
		}
	}

	return o
}

// addOptions returns the attributes every instrument this policy owns carries,
// which is the policy's name or nothing.
func (o *options) addOptions() []metric.AddOption {
	if o.name == "" {
		return nil
	}

	return []metric.AddOption{metric.WithAttributes(attribute.String(nameKey, o.name))}
}

// WithName says what this policy retries, and is worth setting.
//
// A Config is embedded in most of the packages in this module that talk to
// anything over a network, so a deployment runs many of these loops at once. The
// name is the attribute that keeps their counters apart; without one they all
// report as a single number that says retries are happening somewhere.
func WithName(name string) Option {
	return func(o *options) { o.name = name }
}

// WithClock swaps the clock the backoff sleeps against.
//
// Wall time is what production wants. A test does not: an exhausted loop with
// the default schedule spends most of a second asleep, and a suite full of them
// spends it once per case. Inside a testing/synctest bubble the wall clock
// already reads the bubble's fake time, so this is for the deployments that
// inject a clock everywhere rather than for tests that can bubble.
func WithClock(c clock.Clock) Option {
	return func(o *options) {
		if c != nil {
			o.clock = c
		}
	}
}

// WithLogger attaches a logger, which is where an exhausted loop is reported.
func WithLogger(logger logging.Logger) Option {
	return func(o *options) { o.logger = logger }
}

// WithTracerProvider attaches a tracer provider, giving each Execute a span that
// spans every attempt and the waits between them.
func WithTracerProvider(tracerProvider tracing.Provider) Option {
	return func(o *options) { o.tracerProvider = tracerProvider }
}

// WithMetricsProvider attaches a metrics provider for the attempt and
// exhaustion counters.
func WithMetricsProvider(metricsProvider metrics.Provider) Option {
	return func(o *options) { o.metricsProvider = metricsProvider }
}

// WithPillars supplies logger, tracer provider, and metrics provider at once.
//
// Options apply in order, so a WithPillars followed by a narrower option wins
// for that component.
func WithPillars(p *observability.Pillars) Option {
	return func(o *options) {
		o.logger, o.tracerProvider, o.metricsProvider = p.Deps()
	}
}
