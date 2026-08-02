package tracing

import (
	"context"

	"go.opentelemetry.io/otel/trace"
)

var _ Tracer = (*otelTraceWrapper)(nil)

type otelTraceWrapper struct {
	tracer trace.Tracer
}

// NewTracerForTest creates a noop Tracer for use in tests.
func NewTracerForTest(name string) Tracer {
	return NewNamedTracer(nil, name)
}

// NewNamedTracer creates a named Tracer from the given TracerProvider.
// If tracerProvider is nil, a noop TracerProvider is used.
func NewNamedTracer(tracerProvider TracerProvider, name string) Tracer {
	return &otelTraceWrapper{tracer: EnsureTracerProvider(tracerProvider).Tracer(name)}
}

// StartSpan wraps tracer.Start.
func (t *otelTraceWrapper) StartSpan(ctx context.Context) (context.Context, Span) {
	return t.tracer.Start(ctx, GetCallerName())
}

// StartCustomSpan wraps tracer.Start.
func (t *otelTraceWrapper) StartCustomSpan(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, Span) {
	return t.tracer.Start(ctx, name, opts...)
}
