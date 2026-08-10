package timers

import (
	stderrors "errors"
	"strings"
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

type trialID string

type compositeKey struct {
	Tenant string
	Trial  int
}

func TestDefaultKeyCodec(T *testing.T) {
	T.Parallel()

	// A quoted key in the primary key column makes every psql session that
	// touches the table harder to read, for no gain.
	T.Run("stores a string-like key as itself", func(t *testing.T) {
		t.Parallel()

		codec := DefaultKeyCodec[trialID]()

		encoded, err := codec.EncodeKey("t-1")
		must.NoError(t, err)
		test.EqOp(t, "t-1", encoded)

		decoded, err := codec.DecodeKey(encoded)
		must.NoError(t, err)
		test.EqOp(t, trialID("t-1"), decoded)
	})

	T.Run("round-trips a struct key through JSON", func(t *testing.T) {
		t.Parallel()

		codec := DefaultKeyCodec[compositeKey]()
		key := compositeKey{Tenant: "acme", Trial: 7}

		encoded, err := codec.EncodeKey(key)
		must.NoError(t, err)
		test.True(t, strings.Contains(encoded, "acme"))

		decoded, err := codec.DecodeKey(encoded)
		must.NoError(t, err)
		test.EqOp(t, key, decoded)
	})
}

func TestEncodeKey(T *testing.T) {
	T.Parallel()

	codec := DefaultKeyCodec[string]()

	// Two keys that differ only past the limit would become one row, and
	// scheduling the second timer would silently move the first.
	T.Run("rejects an over-long key rather than truncating it", func(t *testing.T) {
		t.Parallel()

		_, err := encodeKey(codec, strings.Repeat("k", MaxKeyLength+1))

		test.True(t, stderrors.Is(err, ErrKeyTooLong))
	})

	// An empty primary key is legal SQL and always a mistake: it is what a
	// zero-valued key encodes to, so admitting it would collapse every unset key
	// onto one row.
	T.Run("rejects an empty key", func(t *testing.T) {
		t.Parallel()

		_, err := encodeKey(codec, "")

		test.True(t, stderrors.Is(err, ErrEmptyKey))
	})

	// The sentinel matters as much as the rejection. A key with a newline in it
	// is malformed, not missing, and reporting it as ErrEmptyKey sent a caller
	// looking for an unset field that was never unset.
	T.Run("rejects control characters", func(t *testing.T) {
		t.Parallel()

		for _, key := range []string{"a\nb", "a\x00b", "a\rb"} {
			_, err := encodeKey(codec, key)

			test.True(t, stderrors.Is(err, ErrKeyContainsControlCharacter), test.Sprintf("key %q", key))
			test.False(t, stderrors.Is(err, ErrEmptyKey), test.Sprintf("key %q", key))
		}
	})

	T.Run("accepts a key at the limit", func(t *testing.T) {
		t.Parallel()

		encoded, err := encodeKey(codec, strings.Repeat("k", MaxKeyLength))

		must.NoError(t, err)
		test.EqOp(t, MaxKeyLength, len(encoded))
	})
}

// The JSON codec's two failure paths. A key type Go accepts as comparable but
// encoding/json cannot render reaches EncodeKey, and a column value that is not
// the JSON this codec wrote reaches DecodeKey — the second is what a table
// written under a different codec looks like.
func TestJSONKeyCodec_Failures(T *testing.T) {
	T.Parallel()

	// Comparable, so it satisfies the constraint, and unmarshalable, so the
	// encoder rejects it.
	type chanKey struct{ C chan int }

	T.Run("reports a key it cannot render", func(t *testing.T) {
		t.Parallel()

		_, err := DefaultKeyCodec[chanKey]().EncodeKey(chanKey{C: make(chan int)})

		must.Error(t, err)
		test.StrContains(t, err.Error(), "encoding timer key")
	})

	T.Run("reports a stored value it cannot read back", func(t *testing.T) {
		t.Parallel()

		_, err := DefaultKeyCodec[compositeKey]().DecodeKey("not json")

		must.Error(t, err)
		test.StrContains(t, err.Error(), "decoding timer key")
	})

	// encodeKey vets what a codec returns, so a codec that fails outright has to
	// surface rather than be read as an empty key.
	T.Run("encodeKey propagates a codec failure rather than vetting it", func(t *testing.T) {
		t.Parallel()

		_, err := encodeKey(DefaultKeyCodec[chanKey](), chanKey{C: make(chan int)})

		must.Error(t, err)
		test.False(t, stderrors.Is(err, ErrEmptyKey))
	})
}
