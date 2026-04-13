package numbers

import (
	"context"
	"testing"

	"github.com/shoenig/test"
)

func TestMinRange_ValidateWithContext(T *testing.T) {
	T.Parallel()

	T.Run("valid", func(t *testing.T) {
		t.Parallel()

		x := &MinRange[float32]{Min: 1.0}
		test.NoError(t, x.ValidateWithContext(context.Background()))
	})

	T.Run("invalid", func(t *testing.T) {
		t.Parallel()

		x := &MinRange[float32]{}
		test.Error(t, x.ValidateWithContext(context.Background()))
	})

	T.Run("valid uint16", func(t *testing.T) {
		t.Parallel()

		x := &MinRange[uint16]{Min: 1}
		test.NoError(t, x.ValidateWithContext(context.Background()))
	})

	T.Run("invalid uint16", func(t *testing.T) {
		t.Parallel()

		x := &MinRange[uint16]{}
		test.Error(t, x.ValidateWithContext(context.Background()))
	})

	T.Run("valid uint32", func(t *testing.T) {
		t.Parallel()

		x := &MinRange[uint32]{Min: 1}
		test.NoError(t, x.ValidateWithContext(context.Background()))
	})

	T.Run("invalid uint32", func(t *testing.T) {
		t.Parallel()

		x := &MinRange[uint32]{}
		test.Error(t, x.ValidateWithContext(context.Background()))
	})
}
