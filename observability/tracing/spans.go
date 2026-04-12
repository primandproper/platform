package tracing

import (
	"context"
	"fmt"
	"net/http"
	"regexp"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

// StartCustomSpan starts an anonymous custom span.
func StartCustomSpan(ctx context.Context, name string) (context.Context, trace.Span) {
	if ctx == nil {
		ctx = context.Background()
	}

	return otel.Tracer("_anon_").Start(ctx, name)
}

// StartSpan starts an anonymous span. Avoid using PackagePrefix and use an embedded tracer where possible.
func StartSpan(ctx context.Context) (context.Context, trace.Span) {
	if ctx == nil {
		ctx = context.Background()
	}

	return otel.Tracer("_anon_").Start(ctx, GetCallerName())
}

var uriIDReplacementRegex = regexp.MustCompile(`/\d+`)

// FormatSpan formats a span.
func FormatSpan(operation string, req *http.Request) string {
	return fmt.Sprintf("%s %s: %s", req.Method, uriIDReplacementRegex.ReplaceAllString(req.URL.Path, "/<id>"), operation)
}

// Span is a simple alias for the OpenTelemetry span interface.
type Span trace.Span
