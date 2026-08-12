package cache

import (
	"testing"
	"time"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// timestampedExample is the case the default cbor.Marshal gets wrong: its
// EncOptions{} encodes time.Time as whole Unix seconds, so a value with
// sub-second precision decodes as a different value.
type timestampedExample struct {
	CreatedAt time.Time `json:"createdAt"`
	Name      string    `json:"name"`
}

func TestCBORCodec(T *testing.T) {
	T.Parallel()

	T.Run("round-trips a value", func(t *testing.T) {
		t.Parallel()

		codec := NewCBORCodec[codecExample]()
		original := &codecExample{Name: "spot", Count: 42}

		encoded, err := codec.Encode(original)
		must.NoError(t, err)
		must.SliceNotEmpty(t, encoded)

		decoded, err := codec.Decode(encoded)
		must.NoError(t, err)
		must.NotNil(t, decoded)
		test.Eq(t, original, decoded)
	})

	T.Run("preserves sub-second time precision", func(t *testing.T) {
		t.Parallel()

		codec := NewCBORCodec[timestampedExample]()
		original := &timestampedExample{
			Name:      "session",
			CreatedAt: time.Date(2026, time.August, 3, 12, 30, 45, 123456789, time.UTC),
		}

		encoded, err := codec.Encode(original)
		must.NoError(t, err)

		decoded, err := codec.Decode(encoded)
		must.NoError(t, err)
		must.NotNil(t, decoded)

		// Compared as an instant rather than with ==: a decoded time carries a
		// fixed zone rather than the original *time.Location, which no portable
		// format preserves.
		test.True(t, original.CreatedAt.Equal(decoded.CreatedAt))
		test.EqOp(t, original.CreatedAt.Nanosecond(), decoded.CreatedAt.Nanosecond())
	})

	T.Run("preserves a non-UTC offset", func(t *testing.T) {
		t.Parallel()

		codec := NewCBORCodec[timestampedExample]()
		original := &timestampedExample{
			Name:      "session",
			CreatedAt: time.Date(2026, time.August, 3, 12, 30, 45, 500, time.FixedZone("", -5*60*60)),
		}

		encoded, err := codec.Encode(original)
		must.NoError(t, err)

		decoded, err := codec.Decode(encoded)
		must.NoError(t, err)
		must.NotNil(t, decoded)

		test.True(t, original.CreatedAt.Equal(decoded.CreatedAt))

		_, offset := decoded.CreatedAt.Zone()
		test.EqOp(t, -5*60*60, offset)
	})

	T.Run("round-trips a zero time", func(t *testing.T) {
		t.Parallel()

		codec := NewCBORCodec[timestampedExample]()
		original := &timestampedExample{Name: "unset"}

		encoded, err := codec.Encode(original)
		must.NoError(t, err)

		decoded, err := codec.Decode(encoded)
		must.NoError(t, err)
		must.NotNil(t, decoded)
		test.True(t, decoded.CreatedAt.IsZero())
	})

	T.Run("round-trips non-UTF8 bytes", func(t *testing.T) {
		t.Parallel()

		type binaryExample struct {
			Payload []byte `json:"payload"`
		}

		codec := NewCBORCodec[binaryExample]()
		// The redis provider stores encoded bytes as a string, so a value
		// holding bytes that are not valid UTF-8 has to survive that trip.
		original := &binaryExample{Payload: []byte{0x00, 0xFF, 0xFE, 0x80, 0x7F}}

		encoded, err := codec.Encode(original)
		must.NoError(t, err)

		decoded, err := codec.Decode([]byte(string(encoded)))
		must.NoError(t, err)
		must.NotNil(t, decoded)
		test.Eq(t, original.Payload, decoded.Payload)
	})

	T.Run("rejects garbage on decode", func(t *testing.T) {
		t.Parallel()

		codec := NewCBORCodec[codecExample]()

		// Deliberately gob's output: a cache warmed by the previous default
		// must fail loudly rather than decode into something plausible.
		gobbed, err := NewGobCodec[codecExample]().Encode(&codecExample{Name: "old", Count: 1})
		must.NoError(t, err)

		_, err = codec.Decode(gobbed)
		test.Error(t, err)
	})

	T.Run("encodes smaller than gob", func(t *testing.T) {
		t.Parallel()

		value := &codecExample{Name: "spot", Count: 42}

		cborEncoded, err := NewCBORCodec[codecExample]().Encode(value)
		must.NoError(t, err)

		gobEncoded, err := NewGobCodec[codecExample]().Encode(value)
		must.NoError(t, err)

		test.Less(t, len(gobEncoded), len(cborEncoded))
	})
}
