package workqueue

import (
	"errors"
	"strings"
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

type orderID string

type pairKey struct {
	Profile string
	Origin  int64
	Dest    int64
}

func TestDefaultKeyCodec(T *testing.T) {
	T.Parallel()

	T.Run("stores a plain string as itself", func(t *testing.T) {
		t.Parallel()

		codec := DefaultKeyCodec[string]()

		encoded, err := codec.EncodeKey("orders/17")
		must.NoError(t, err)
		test.EqOp(t, "orders/17", encoded)

		decoded, err := codec.DecodeKey(encoded)
		must.NoError(t, err)
		test.EqOp(t, "orders/17", decoded)
	})

	// The named type is the interesting half: a ~string key is the common shape
	// for a domain identifier, and JSON-quoting it would make the table
	// illegible for no benefit.
	T.Run("stores a named string type as itself", func(t *testing.T) {
		t.Parallel()

		codec := DefaultKeyCodec[orderID]()

		encoded, err := codec.EncodeKey(orderID("abc"))
		must.NoError(t, err)
		test.EqOp(t, "abc", encoded)

		decoded, err := codec.DecodeKey(encoded)
		must.NoError(t, err)
		test.EqOp(t, orderID("abc"), decoded)
	})

	T.Run("round-trips a struct key through JSON", func(t *testing.T) {
		t.Parallel()

		codec := DefaultKeyCodec[pairKey]()
		key := pairKey{Profile: "car", Origin: 4, Dest: 9}

		encoded, err := codec.EncodeKey(key)
		must.NoError(t, err)
		test.True(t, strings.Contains(encoded, "car"))

		decoded, err := codec.DecodeKey(encoded)
		must.NoError(t, err)
		test.EqOp(t, key, decoded)
	})

	// The rendering is the row's identity, so it has to be a pure function of
	// the key. If it were not, the same key would enqueue as two rows and each
	// would be worked separately.
	T.Run("renders one key identically every time", func(t *testing.T) {
		t.Parallel()

		codec := DefaultKeyCodec[pairKey]()
		key := pairKey{Profile: "bike", Origin: 1, Dest: 2}

		first, err := codec.EncodeKey(key)
		must.NoError(t, err)

		second, err := codec.EncodeKey(key)
		must.NoError(t, err)

		test.EqOp(t, first, second)
	})

	T.Run("distinct keys render distinctly", func(t *testing.T) {
		t.Parallel()

		codec := DefaultKeyCodec[pairKey]()

		a, err := codec.EncodeKey(pairKey{Profile: "car", Origin: 1, Dest: 2})
		must.NoError(t, err)

		b, err := codec.EncodeKey(pairKey{Profile: "car", Origin: 2, Dest: 1})
		must.NoError(t, err)

		test.NotEqOp(t, a, b)
	})

	T.Run("reports a key it cannot decode", func(t *testing.T) {
		t.Parallel()

		_, err := DefaultKeyCodec[pairKey]().DecodeKey("not json")
		test.Error(t, err)
	})

	T.Run("integer keys survive the JSON path", func(t *testing.T) {
		t.Parallel()

		codec := DefaultKeyCodec[int64]()

		encoded, err := codec.EncodeKey(1234)
		must.NoError(t, err)
		test.EqOp(t, "1234", encoded)

		decoded, err := codec.DecodeKey(encoded)
		must.NoError(t, err)
		test.EqOp(t, int64(1234), decoded)
	})
}

func TestEncodeKey(T *testing.T) {
	T.Parallel()

	T.Run("accepts an ordinary key", func(t *testing.T) {
		t.Parallel()

		encoded, err := encodeKey(DefaultKeyCodec[string](), "fine")
		must.NoError(t, err)
		test.EqOp(t, "fine", encoded)
	})

	T.Run("rejects a key that encodes to nothing", func(t *testing.T) {
		t.Parallel()

		_, err := encodeKey(DefaultKeyCodec[string](), "")
		test.ErrorIs(t, err, ErrEmptyKey)
	})

	T.Run("rejects an over-long key", func(t *testing.T) {
		t.Parallel()

		_, err := encodeKey(DefaultKeyCodec[string](), strings.Repeat("k", MaxKeyLength+1))
		test.ErrorIs(t, err, ErrKeyTooLong)
	})

	T.Run("accepts a key exactly at the limit", func(t *testing.T) {
		t.Parallel()

		_, err := encodeKey(DefaultKeyCodec[string](), strings.Repeat("k", MaxKeyLength))
		test.NoError(t, err)
	})

	// The sentinel matters as much as the rejection. A key with a newline in it
	// is malformed, not missing, and reporting it as ErrEmptyKey sent a caller
	// looking for an unset field that was never unset.
	T.Run("rejects control characters", func(t *testing.T) {
		t.Parallel()

		for _, key := range []string{"a\nb", "a\x00b", "a\rb"} {
			_, err := encodeKey(DefaultKeyCodec[string](), key)
			test.ErrorIs(t, err, ErrKeyContainsControlCharacter, test.Sprintf("key %q", key))
			test.False(t, errors.Is(err, ErrEmptyKey), test.Sprintf("key %q", key))
		}
	})

	// The vetting lives outside the codecs so that a caller supplying their own
	// rendering inherits it rather than having to remember the column's limits.
	T.Run("vets a custom codec's output too", func(t *testing.T) {
		t.Parallel()

		_, err := encodeKey(overlongCodec{}, "short")
		test.ErrorIs(t, err, ErrKeyTooLong)
	})

	T.Run("surfaces a codec's own failure", func(t *testing.T) {
		t.Parallel()

		_, err := encodeKey(DefaultKeyCodec[chan int](), nil)
		test.Error(t, err)
	})
}

func TestEncodeKeys(T *testing.T) {
	T.Parallel()

	T.Run("preserves the caller's order", func(t *testing.T) {
		t.Parallel()

		encoded, err := encodeKeys(DefaultKeyCodec[string](), []string{"c", "a", "b"})
		must.NoError(t, err)
		test.Eq(t, []string{"c", "a", "b"}, encoded)
	})

	T.Run("fails the whole batch on one bad key", func(t *testing.T) {
		t.Parallel()

		_, err := encodeKeys(DefaultKeyCodec[string](), []string{"a", "", "b"})
		test.ErrorIs(t, err, ErrEmptyKey)
	})
}

// overlongCodec renders every key past the column's limit, to prove the vetting
// applies to a custom codec and not only to the defaults.
type overlongCodec struct{}

func (overlongCodec) EncodeKey(string) (string, error) {
	return strings.Repeat("x", MaxKeyLength+10), nil
}

func (overlongCodec) DecodeKey(string) (string, error) { return "", nil }
