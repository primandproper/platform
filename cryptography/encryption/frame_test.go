package encryption

import (
	"strings"
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestEncodeHeader(T *testing.T) {
	T.Parallel()

	T.Run("renders version, length, and ID", func(t *testing.T) {
		t.Parallel()

		header, err := encodeHeader("k1")
		must.NoError(t, err)

		test.Eq(t, []byte{frameVersion, 2, 'k', '1'}, header)
	})

	T.Run("rejects an empty ID", func(t *testing.T) {
		t.Parallel()

		_, err := encodeHeader("")
		test.ErrorIs(t, err, ErrEmptyKeyID)
	})

	T.Run("rejects an ID past the length byte", func(t *testing.T) {
		t.Parallel()

		_, err := encodeHeader(KeyID(strings.Repeat("k", MaxKeyIDLength+1)))
		test.ErrorIs(t, err, ErrKeyIDTooLong)
	})

	T.Run("accepts an ID at exactly the limit", func(t *testing.T) {
		t.Parallel()

		// The boundary is worth pinning because the length is stored in one
		// byte: 255 fits and 256 wraps to zero, which would encode as an empty
		// key ID rather than failing.
		_, err := encodeHeader(KeyID(strings.Repeat("k", MaxKeyIDLength)))
		test.NoError(t, err)
	})
}

func TestDecodeHeader(T *testing.T) {
	T.Parallel()

	T.Run("round trips what encodeHeader produced", func(t *testing.T) {
		t.Parallel()

		header, err := encodeHeader("k1")
		must.NoError(t, err)

		id, gotHeader, body, err := decodeHeader(append(header, 'b', 'o', 'd', 'y'))
		must.NoError(t, err)

		test.EqOp(t, KeyID("k1"), id)
		test.Eq(t, header, gotHeader)
		test.Eq(t, []byte("body"), body)
	})

	T.Run("rejects a ciphertext too short to hold a header", func(t *testing.T) {
		t.Parallel()

		_, _, _, err := decodeHeader([]byte{frameVersion})
		test.ErrorIs(t, err, ErrMalformedCiphertext)
	})

	T.Run("rejects an unknown version", func(t *testing.T) {
		t.Parallel()

		// Refusing rather than guessing is the point of the byte: a future
		// layout read as this one would decrypt garbage or panic.
		_, _, _, err := decodeHeader([]byte{frameVersion + 1, 2, 'k', '1'})
		test.ErrorIs(t, err, ErrUnsupportedCiphertextVersion)
	})

	T.Run("rejects a declared empty key ID", func(t *testing.T) {
		t.Parallel()

		_, _, _, err := decodeHeader([]byte{frameVersion, 0, 'b'})
		test.ErrorIs(t, err, ErrMalformedCiphertext)
	})

	T.Run("rejects a key ID length that runs off the end", func(t *testing.T) {
		t.Parallel()

		_, _, _, err := decodeHeader([]byte{frameVersion, 8, 'k', '1'})
		test.ErrorIs(t, err, ErrMalformedCiphertext)
	})

	T.Run("accepts an empty body", func(t *testing.T) {
		t.Parallel()

		// A zero-length body is not this layer's problem — whether it can be
		// opened is the Cipher's call.
		id, _, body, err := decodeHeader([]byte{frameVersion, 2, 'k', '1'})
		must.NoError(t, err)

		test.EqOp(t, KeyID("k1"), id)
		test.SliceEmpty(t, body)
	})
}

func TestBindHeader(T *testing.T) {
	T.Parallel()

	T.Run("concatenates header and associated data", func(t *testing.T) {
		t.Parallel()

		test.Eq(t, []byte("hdraad"), bindHeader([]byte("hdr"), []byte("aad")))
	})

	T.Run("copies rather than aliasing the header", func(t *testing.T) {
		t.Parallel()

		// The regression this guards: Encrypt calls bindHeader and then
		// appends the sealed bytes to the same header slice. If bindHeader
		// returned something backed by the header's array, the second append
		// would overwrite the associated data that was already authenticated —
		// but only when the header happened to have spare capacity, which is
		// to say intermittently.
		header := make([]byte, 0, 64)
		header = append(header, "hdr"...)

		bound := bindHeader(header, []byte("aad"))
		header = append(header, "clobber"...)
		_ = header

		test.Eq(t, []byte("hdraad"), bound)
	})

	T.Run("tolerates absent associated data", func(t *testing.T) {
		t.Parallel()

		test.Eq(t, []byte("hdr"), bindHeader([]byte("hdr"), nil))
	})
}
