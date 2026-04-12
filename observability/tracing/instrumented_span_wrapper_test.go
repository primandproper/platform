package tracing

import (
	"errors"
	"testing"
)

func Test_instrumentedSQLSpanWrapper_NewChild(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		ctx, span := StartSpan(t.Context())
		w := &instrumentedSQLSpanWrapper{
			ctx:    ctx,
			tracer: NewTracer(NewNoopTracerProvider().Tracer(t.Name())),
			span:   span,
		}

		w.NewChild("test")
	})
}

func Test_instrumentedSQLSpanWrapper_SetLabel(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		ctx, span := StartSpan(t.Context())
		w := &instrumentedSQLSpanWrapper{
			ctx:  ctx,
			span: span,
		}

		w.SetLabel("things", "stuff")
	})
}

func Test_instrumentedSQLSpanWrapper_SetError(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		ctx, span := StartSpan(t.Context())
		w := &instrumentedSQLSpanWrapper{
			ctx:  ctx,
			span: span,
		}

		w.SetError(errors.New("blah"))
	})
}

func Test_instrumentedSQLSpanWrapper_Finish(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		ctx, span := StartSpan(t.Context())
		w := &instrumentedSQLSpanWrapper{
			ctx:  ctx,
			span: span,
		}

		w.Finish()
	})
}
