package tracing

import (
	"context"

	"github.com/primandproper/platform-go/v10/observability/logging"
	"github.com/primandproper/platform-go/v10/observability/logging/slog"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

type errorHandler struct {
	logger logging.Logger
}

func (h errorHandler) Handle(err error) {
	h.logger.Error("tracer reported issue", err)
}

func init() {
	// set a noop error handler just so one is set
	otel.SetErrorHandler(errorHandler{logger: slog.NewSlogLogger(logging.ErrorLevel).WithName("otel_errors")})
}

// Tracer describes a tracer.
type Tracer interface {
	StartSpan(ctx context.Context) (context.Context, Span)
	StartCustomSpan(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, Span)
}

// Provider is trace.TracerProvider plus the two lifecycle methods a
// process needs at exit.
//
// ForceFlush drains what is buffered; Shutdown drains and then releases — it
// stops the span processor's goroutine and closes the exporter's connection.
// Flushing alone leaves both running, which is why Shutdown is part of the
// interface rather than something callers type-assert for: an implementation
// that cannot be shut down is one this package should not accept.
type Provider interface {
	trace.TracerProvider
	ForceFlush(context.Context) error
	Shutdown(context.Context) error
}

type noopProvider struct {
	noop.TracerProvider
}

func (n *noopProvider) Tracer(instrumentationName string, opts ...trace.TracerOption) trace.Tracer {
	return noop.NewTracerProvider().Tracer(instrumentationName, opts...)
}

func (n *noopProvider) ForceFlush(context.Context) error {
	return nil
}

func (n *noopProvider) Shutdown(context.Context) error {
	return nil
}

func EnsureTracerProvider(tracerProvider Provider) Provider {
	if tracerProvider != nil {
		return tracerProvider
	}

	return &noopProvider{}
}
