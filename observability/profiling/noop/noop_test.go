package noop

import (
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestNewProvider(T *testing.T) {
	T.Parallel()

	T.Run("returns non-nil provider", func(t *testing.T) {
		t.Parallel()

		p := NewProvider()
		must.NotNil(t, p)
	})
}

func TestProvider_Start(T *testing.T) {
	T.Parallel()

	T.Run("returns no error", func(t *testing.T) {
		t.Parallel()

		p := NewProvider()
		test.NoError(t, p.Start(t.Context()))
	})
}

func TestProvider_Shutdown(T *testing.T) {
	T.Parallel()

	T.Run("returns no error", func(t *testing.T) {
		t.Parallel()

		p := NewProvider()
		test.NoError(t, p.Shutdown(t.Context()))
	})
}
