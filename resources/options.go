package resources

import (
	platformerrors "github.com/primandproper/platform-go/v12/errors"
	"github.com/primandproper/platform-go/v12/identifiers"
	"github.com/primandproper/platform-go/v12/observability"
	"github.com/primandproper/platform-go/v12/observability/logging"
	"github.com/primandproper/platform-go/v12/observability/metrics"
	"github.com/primandproper/platform-go/v12/observability/tracing"
)

// ErrHookTypeMismatch indicates a hook built for one row type handed to a store
// for another.
//
// Option carries no type parameter, so the compiler cannot catch it. NewStore
// does, at construction — which is startup, not a request.
var ErrHookTypeMismatch = platformerrors.New("hook is for a different row type than the store")

// Option configures a Store at construction.
//
// It carries no type parameter even though the Store does. Go cannot infer a
// type argument from a call's result type, so an Option[T] would force every
// call site to spell the row type out by hand — WithLogger[Comment](l) — at
// every option, forever. WithHook is the one option that depends on the row
// type; it stays generic and still needs no annotation, because T is inferable
// from the hook it is handed.
type Option func(*options)

// options accumulates what the options set, so Option can stay free of the
// Store's type parameter.
type options struct {
	logger          logging.Logger
	tracerProvider  tracing.Provider
	metricsProvider metrics.Provider
	newID           func() string

	// hooks holds Hook[T] values for the T of the store being built. They are
	// typed as any because Option cannot name T; NewStore asserts them back and
	// reports a mismatch rather than dropping them.
	hooks []any
}

// WithHook registers a hook to run inside the transaction of every write.
//
// This is where an application's conventions attach. Audit entries, outbox
// events, search-index stamps, cache invalidations: none of them belong to this
// package, and all of them belong in the transaction that wrote the row. A
// resource with no hooks writes rows and nothing else.
//
// T is inferred from the hook, so this needs no type argument:
//
//	resources.WithHook(audithook.Record[Comment](recorder))
//
// Hooks run in registration order, and the first error stops the rest and rolls
// the transaction back.
func WithHook[T any](hook Hook[T]) Option {
	return func(o *options) {
		if hook != nil {
			o.hooks = append(o.hooks, hook)
		}
	}
}

// WithIDFactory replaces the source of identifiers for rows created without one.
//
// The default is identifiers.New, whose output sorts by creation time — which
// the cursor walk requires, since the id is also the pagination cursor. A
// replacement that does not sort that way produces pages in an order nobody
// asked for, so this exists for applications with their own time-sortable
// scheme, and for tests that want a predictable sequence.
func WithIDFactory(factory func() string) Option {
	return func(o *options) {
		if factory != nil {
			o.newID = factory
		}
	}
}

// WithLogger attaches a logger. An absent logger logs nowhere.
func WithLogger(logger logging.Logger) Option {
	return func(o *options) { o.logger = logger }
}

// WithTracerProvider attaches a tracer provider, so a store's operations show up
// as children of the span that owns the request. An absent one traces nowhere.
func WithTracerProvider(tracerProvider tracing.Provider) Option {
	return func(o *options) { o.tracerProvider = tracerProvider }
}

// WithMetricsProvider attaches a metrics provider. An absent one records
// nowhere.
//
// What it records is the trio every component in this module records — attempts,
// failures, latency — under one "resources" prefix, with the resource and the
// method as attributes rather than as instrument names. A deployment's comment
// store and its waitlist store are then two series in one panel instead of two
// panels, and a domain declared next week is in that panel already.
func WithMetricsProvider(metricsProvider metrics.Provider) Option {
	return func(o *options) { o.metricsProvider = metricsProvider }
}

// WithPillars supplies the logger, tracer provider, and metrics provider at
// once.
//
// Options apply in order, so a caller wanting all of it but one names the
// pillars and then overrides: WithPillars(p) followed by
// WithMetricsProvider(nil) leaves this one store unmetered.
func WithPillars(pillars *observability.Pillars) Option {
	return func(o *options) {
		o.logger, o.tracerProvider, o.metricsProvider = pillars.Deps()
	}
}

// defaultIDFactory is identifiers.New, named so the option's documentation has
// something to point at.
func defaultIDFactory() string { return identifiers.New() }
