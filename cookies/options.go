package cookies

import (
	"github.com/primandproper/platform-go/v9/observability/tracing"
)

// Option configures the Manager this package constructs. The zero
// configuration works: an absent tracer provider traces nowhere.
type Option func(*options)

type options struct {
	tracerProvider tracing.TracerProvider
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

// WithTracerProvider attaches a tracer provider, enabling spans on every
// operation.
func WithTracerProvider(tracerProvider tracing.TracerProvider) Option {
	return func(o *options) { o.tracerProvider = tracerProvider }
}
