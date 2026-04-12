package noop

import (
	"testing"

	"github.com/shoenig/test"
)

func TestNewCircuitBreaker(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		x := NewCircuitBreaker()
		test.NotNil(t, x)
	})
}

func TestCircuitBreaker_Failed(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		x := NewCircuitBreaker()
		x.Failed()
	})
}

func TestCircuitBreaker_Succeeded(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		x := NewCircuitBreaker()
		x.Succeeded()
	})
}

func TestCircuitBreaker_CanProceed(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		x := NewCircuitBreaker()
		test.True(t, x.CanProceed())
	})
}

func TestCircuitBreaker_CannotProceed(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		x := NewCircuitBreaker()
		test.False(t, x.CannotProceed())
	})
}
