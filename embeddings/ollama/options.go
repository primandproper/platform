package ollama

import (
	"github.com/primandproper/platform-go/v8/observability/logging"
	"github.com/primandproper/platform-go/v8/observability/tracing"
)

// Option configures the embedder this package constructs. The zero
// configuration works: an absent logger logs nowhere and an absent tracer
// traces nowhere.
type Option func(*options)

type options struct {
	logger logging.Logger
	tracer tracing.Tracer
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

// WithLogger attaches a logger.
func WithLogger(logger logging.Logger) Option {
	return func(o *options) { o.logger = logger }
}

// WithTracer attaches a tracer, enabling spans on every operation.
func WithTracer(tracer tracing.Tracer) Option {
	return func(o *options) { o.tracer = tracer }
}
