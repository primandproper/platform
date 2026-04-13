package noop

import (
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestNewTracerProvider(T *testing.T) {
	T.Parallel()

	T.Run("returns non-nil provider", func(t *testing.T) {
		t.Parallel()

		tp := NewTracerProvider()
		must.NotNil(t, tp)
	})
}

func TestTracerProvider_Tracer(T *testing.T) {
	T.Parallel()

	T.Run("returns non-nil tracer", func(t *testing.T) {
		t.Parallel()

		tp := NewTracerProvider()
		tracer := tp.Tracer("test")

		test.NotNil(t, tracer)
	})
}

func TestTracerProvider_ForceFlush(T *testing.T) {
	T.Parallel()

	T.Run("returns no error", func(t *testing.T) {
		t.Parallel()

		tp := NewTracerProvider()
		test.NoError(t, tp.ForceFlush(t.Context()))
	})
}
