package cache

import (
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

type codecExample struct {
	Name  string
	Count int
}

func TestGobCodec(T *testing.T) {
	T.Parallel()

	T.Run("round-trips a value", func(t *testing.T) {
		t.Parallel()

		codec := NewGobCodec[codecExample]()
		original := &codecExample{Name: "beeline", Count: 42}

		encoded, err := codec.Encode(original)
		must.NoError(t, err)
		must.SliceNotEmpty(t, encoded)

		decoded, err := codec.Decode(encoded)
		must.NoError(t, err)
		must.NotNil(t, decoded)
		test.Eq(t, original, decoded)
	})

	T.Run("rejects garbage on decode", func(t *testing.T) {
		t.Parallel()

		codec := NewGobCodec[codecExample]()

		_, err := codec.Decode([]byte("not gob data"))
		test.Error(t, err)
	})
}
