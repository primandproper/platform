package profiling

import (
	"context"
	"testing"

	"github.com/shoenig/test"
)

func TestNewNoopProvider(T *testing.T) {
	T.Parallel()

	T.Run("returns non-nil provider", func(t *testing.T) {
		t.Parallel()
		p := NewNoopProvider()
		test.NotNil(t, p)
	})

	T.Run("Start returns nil", func(t *testing.T) {
		t.Parallel()
		p := NewNoopProvider()
		test.NoError(t, p.Start(context.Background()))
	})

	T.Run("Shutdown returns nil", func(t *testing.T) {
		t.Parallel()
		p := NewNoopProvider()
		test.NoError(t, p.Shutdown(context.Background()))
	})
}
