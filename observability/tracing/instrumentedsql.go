package tracing

import (
	"context"

	"github.com/luna-duclos/instrumentedsql"
)

// NewInstrumentedSQLTracer wraps a Tracer for instrumentedsql.
func NewInstrumentedSQLTracer(tracerProvider TracerProvider, name string) instrumentedsql.Tracer {
	return &instrumentedSQLTracerWrapper{tracer: NewTracer(tracerProvider.Tracer(name))}
}

var _ instrumentedsql.Tracer = (*instrumentedSQLTracerWrapper)(nil)

type instrumentedSQLTracerWrapper struct {
	tracer Tracer
}

// GetSpan wraps tracer.GetSpan.
func (t *instrumentedSQLTracerWrapper) GetSpan(ctx context.Context) instrumentedsql.Span {
	ctx, span := t.tracer.StartSpan(ctx)

	return &instrumentedSQLSpanWrapper{
		ctx:    ctx,
		tracer: t.tracer,
		span:   span,
	}
}
