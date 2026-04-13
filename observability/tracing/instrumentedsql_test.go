package tracing

import (
	"testing"

	"github.com/shoenig/test"
)

func TestNewInstrumentedSQLTracer(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		test.NotNil(t, NewInstrumentedSQLTracer(&noopTracerProvider{}, t.Name()))
	})
}

func Test_instrumentedSQLTracerWrapper_GetSpan(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		w := NewInstrumentedSQLTracer(&noopTracerProvider{}, t.Name())

		test.NotNil(t, w.GetSpan(ctx))
	})
}
