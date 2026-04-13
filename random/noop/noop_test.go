package noop

import (
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestNewGenerator(T *testing.T) {
	T.Parallel()

	T.Run("returns non-nil generator", func(t *testing.T) {
		t.Parallel()

		g := NewGenerator()
		must.NotNil(t, g)
	})
}

func TestGenerator_GenerateHexEncodedString(T *testing.T) {
	T.Parallel()

	T.Run("returns empty string and no error", func(t *testing.T) {
		t.Parallel()

		g := NewGenerator()
		s, err := g.GenerateHexEncodedString(t.Context(), 32)

		must.NoError(t, err)
		test.EqOp(t, "", s)
	})
}

func TestGenerator_GenerateBase32EncodedString(T *testing.T) {
	T.Parallel()

	T.Run("returns empty string and no error", func(t *testing.T) {
		t.Parallel()

		g := NewGenerator()
		s, err := g.GenerateBase32EncodedString(t.Context(), 32)

		must.NoError(t, err)
		test.EqOp(t, "", s)
	})
}

func TestGenerator_GenerateBase64EncodedString(T *testing.T) {
	T.Parallel()

	T.Run("returns empty string and no error", func(t *testing.T) {
		t.Parallel()

		g := NewGenerator()
		s, err := g.GenerateBase64EncodedString(t.Context(), 32)

		must.NoError(t, err)
		test.EqOp(t, "", s)
	})
}

func TestGenerator_GenerateRawBytes(T *testing.T) {
	T.Parallel()

	T.Run("returns empty bytes and no error", func(t *testing.T) {
		t.Parallel()

		g := NewGenerator()
		b, err := g.GenerateRawBytes(t.Context(), 32)

		must.NoError(t, err)
		test.SliceEmpty(t, b)
		test.NotNil(t, b)
	})
}
