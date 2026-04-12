package random

import (
	"errors"
	"testing"

	"github.com/primandproper/platform/observability/tracing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

type erroneousReader struct{}

func (r *erroneousReader) Read(p []byte) (n int, err error) {
	return -1, errors.New("blah")
}

func TestGenerateBase32EncodedString(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()

		actual, err := GenerateBase32EncodedString(ctx, 32)
		test.NoError(t, err)
		test.NotEq(t, "", actual)
	})
}

func TestGenerateBase64EncodedString(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()

		actual, err := GenerateBase64EncodedString(ctx, 32)
		test.NoError(t, err)
		test.NotEq(t, "", actual)
	})
}

func TestGenerateRawBytes(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()

		actual, err := GenerateRawBytes(ctx, 32)
		test.NoError(t, err)
		test.SliceNotEmpty(t, actual)
	})
}

func TestStandardSecretGenerator_GenerateBase32EncodedString(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		exampleLength := 123

		s := NewGenerator(nil, tracing.NewNoopTracerProvider())
		value, err := s.GenerateBase32EncodedString(ctx, exampleLength)

		test.NotEq(t, "", value)
		test.Greater(t, exampleLength, len(value))
		test.NoError(t, err)
	})

	T.Run("with error reading from secure PRNG", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		exampleLength := 123

		s, ok := NewGenerator(nil, tracing.NewNoopTracerProvider()).(*standardGenerator)
		must.True(t, ok)
		s.randReader = &erroneousReader{}
		value, err := s.GenerateBase32EncodedString(ctx, exampleLength)

		test.EqOp(t, "", value)
		test.Error(t, err)
	})
}

func TestStandardSecretGenerator_GenerateBase64EncodedString(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		exampleLength := 123

		s := NewGenerator(nil, tracing.NewNoopTracerProvider())
		value, err := s.GenerateBase64EncodedString(ctx, exampleLength)

		test.NotEq(t, "", value)
		test.Greater(t, exampleLength, len(value))
		test.NoError(t, err)
	})

	T.Run("with error reading from secure PRNG", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		exampleLength := 123

		s, ok := NewGenerator(nil, tracing.NewNoopTracerProvider()).(*standardGenerator)
		must.True(t, ok)
		s.randReader = &erroneousReader{}
		value, err := s.GenerateBase64EncodedString(ctx, exampleLength)

		test.EqOp(t, "", value)
		test.Error(t, err)
	})
}

func TestStandardSecretGenerator_GenerateRawBytes(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		exampleLength := 123

		s := NewGenerator(nil, tracing.NewNoopTracerProvider())
		value, err := s.GenerateRawBytes(ctx, exampleLength)

		test.SliceNotEmpty(t, value)
		test.EqOp(t, exampleLength, len(value))
		test.NoError(t, err)
	})

	T.Run("with error reading from secure PRNG", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		exampleLength := 123

		s, ok := NewGenerator(nil, tracing.NewNoopTracerProvider()).(*standardGenerator)
		must.True(t, ok)
		s.randReader = &erroneousReader{}
		value, err := s.GenerateRawBytes(ctx, exampleLength)

		test.SliceEmpty(t, value)
		test.Error(t, err)
	})
}

func TestMustGenerateRawBytes(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()

		result := MustGenerateRawBytes(ctx, 32)
		test.SliceNotEmpty(t, result)
	})
}

func TestGenerateHexEncodedString(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()

		result, err := GenerateHexEncodedString(ctx, 32)
		test.NoError(t, err)
		test.NotEq(t, "", result)
	})
}
