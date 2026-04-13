package noop

import (
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestNewBuilder(T *testing.T) {
	T.Parallel()

	T.Run("returns non-nil builder", func(t *testing.T) {
		t.Parallel()

		b := NewBuilder()
		must.NotNil(t, b)
	})
}

func TestBuilder_BuildQRCode(T *testing.T) {
	T.Parallel()

	T.Run("returns empty string and no error", func(t *testing.T) {
		t.Parallel()

		b := NewBuilder()
		result, err := b.BuildQRCode(t.Context(), "user@example.com", "JBSWY3DPEHPK3PXP")

		must.NoError(t, err)
		test.EqOp(t, "", result)
	})
}
