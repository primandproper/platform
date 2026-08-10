package encryption

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// fakeCipher is a reversible stand-in that authenticates by remembering what
// it was given. It is not encryption and does not pretend to be — these tests
// are about the ring's framing and dispatch, and a real cipher would only
// make a failure harder to read.
type fakeCipher struct {
	tag byte
}

func (c fakeCipher) Seal(_ context.Context, plaintext, associatedData []byte) ([]byte, error) {
	out := []byte{c.tag, byte(len(associatedData))}
	out = append(out, associatedData...)

	return append(out, plaintext...), nil
}

func (c fakeCipher) Open(_ context.Context, ciphertext, associatedData []byte) ([]byte, error) {
	if len(ciphertext) < 2 || ciphertext[0] != c.tag {
		return nil, ErrAuthenticationFailed
	}

	aadLen := int(ciphertext[1])
	if len(ciphertext) < 2+aadLen {
		return nil, ErrMalformedCiphertext
	}

	if !bytes.Equal(ciphertext[2:2+aadLen], associatedData) {
		return nil, ErrAuthenticationFailed
	}

	return ciphertext[2+aadLen:], nil
}

func twoKeyRing(t *testing.T) *Keyring {
	t.Helper()

	ring, err := NewKeyring("k2", []RingKey{
		{ID: "k1", Cipher: fakeCipher{tag: 1}},
		{ID: "k2", Cipher: fakeCipher{tag: 2}},
	})
	must.NoError(t, err)

	return ring
}

func TestNewKeyring(T *testing.T) {
	T.Parallel()

	T.Run("builds over several keys", func(t *testing.T) {
		t.Parallel()

		ring := twoKeyRing(t)

		test.EqOp(t, KeyID("k2"), ring.CurrentKeyID())
		test.SliceLen(t, 2, ring.KeyIDs())
	})

	T.Run("rejects an empty ring", func(t *testing.T) {
		t.Parallel()

		_, err := NewKeyring("k1", nil)
		test.ErrorIs(t, err, ErrEmptyKeyring)
	})

	T.Run("rejects a current key that is not in the ring", func(t *testing.T) {
		t.Parallel()

		// Silently promoting some other key would mean a typo in configuration
		// quietly encrypts production under the wrong one.
		_, err := NewKeyring("k9", []RingKey{{ID: "k1", Cipher: fakeCipher{tag: 1}}})
		test.ErrorIs(t, err, ErrNoCurrentKey)
	})

	T.Run("rejects duplicate key IDs", func(t *testing.T) {
		t.Parallel()

		_, err := NewKeyring("k1", []RingKey{
			{ID: "k1", Cipher: fakeCipher{tag: 1}},
			{ID: "k1", Cipher: fakeCipher{tag: 2}},
		})
		test.ErrorIs(t, err, ErrDuplicateKeyID)
	})

	T.Run("rejects an empty key ID", func(t *testing.T) {
		t.Parallel()

		_, err := NewKeyring("", []RingKey{{ID: "", Cipher: fakeCipher{tag: 1}}})
		test.ErrorIs(t, err, ErrEmptyKeyID)
	})

	T.Run("rejects an over-long key ID", func(t *testing.T) {
		t.Parallel()

		id := KeyID(strings.Repeat("k", MaxKeyIDLength+1))

		_, err := NewKeyring(id, []RingKey{{ID: id, Cipher: fakeCipher{tag: 1}}})
		test.ErrorIs(t, err, ErrKeyIDTooLong)
	})

	T.Run("rejects a key with no cipher", func(t *testing.T) {
		t.Parallel()

		_, err := NewKeyring("k1", []RingKey{{ID: "k1"}})
		test.ErrorIs(t, err, ErrNilCipher)
	})
}

func TestKeyring_RoundTrip(T *testing.T) {
	T.Parallel()

	T.Run("encrypts and decrypts", func(t *testing.T) {
		t.Parallel()

		ring := twoKeyRing(t)

		ciphertext, err := ring.Encrypt(t.Context(), []byte("secret"), []byte("row-7"))
		must.NoError(t, err)

		plaintext, err := ring.Decrypt(t.Context(), ciphertext, []byte("row-7"))
		must.NoError(t, err)

		test.Eq(t, []byte("secret"), plaintext)
	})

	T.Run("writes under the current key", func(t *testing.T) {
		t.Parallel()

		ring := twoKeyRing(t)

		ciphertext, err := ring.Encrypt(t.Context(), []byte("secret"), nil)
		must.NoError(t, err)

		id, _, _, err := decodeHeader(ciphertext)
		must.NoError(t, err)

		test.EqOp(t, KeyID("k2"), id)
	})

	T.Run("survives an absent associated data on both sides", func(t *testing.T) {
		t.Parallel()

		ring := twoKeyRing(t)

		ciphertext, err := ring.Encrypt(t.Context(), []byte("secret"), nil)
		must.NoError(t, err)

		plaintext, err := ring.Decrypt(t.Context(), ciphertext, nil)
		must.NoError(t, err)

		test.Eq(t, []byte("secret"), plaintext)
	})

	T.Run("round trips an empty plaintext", func(t *testing.T) {
		t.Parallel()

		ring := twoKeyRing(t)

		ciphertext, err := ring.Encrypt(t.Context(), nil, nil)
		must.NoError(t, err)

		plaintext, err := ring.Decrypt(t.Context(), ciphertext, nil)
		must.NoError(t, err)

		test.SliceEmpty(t, plaintext)
	})
}

func TestKeyring_Rotation(T *testing.T) {
	T.Parallel()

	T.Run("a ciphertext written under a retired key still opens", func(t *testing.T) {
		t.Parallel()

		// This is the entire point of the ring. Writing under k1, rotating to
		// k2, and still reading the old row is what makes rotation something
		// other than a flag day.
		old, err := NewKeyring("k1", []RingKey{{ID: "k1", Cipher: fakeCipher{tag: 1}}})
		must.NoError(t, err)

		ciphertext, err := old.Encrypt(t.Context(), []byte("secret"), []byte("row-7"))
		must.NoError(t, err)

		rotated := twoKeyRing(t)

		plaintext, err := rotated.Decrypt(t.Context(), ciphertext, []byte("row-7"))
		must.NoError(t, err)

		test.Eq(t, []byte("secret"), plaintext)
	})

	T.Run("a ciphertext naming a dropped key is refused", func(t *testing.T) {
		t.Parallel()

		old, err := NewKeyring("k1", []RingKey{{ID: "k1", Cipher: fakeCipher{tag: 1}}})
		must.NoError(t, err)

		ciphertext, err := old.Encrypt(t.Context(), []byte("secret"), nil)
		must.NoError(t, err)

		// k1 retired before everything it wrote was re-encrypted. The data is
		// not recoverable and the error has to say so rather than look like
		// corruption.
		narrowed, err := NewKeyring("k2", []RingKey{{ID: "k2", Cipher: fakeCipher{tag: 2}}})
		must.NoError(t, err)

		_, err = narrowed.Decrypt(t.Context(), ciphertext, nil)
		test.ErrorIs(t, err, ErrUnknownKeyID)
	})
}

func TestKeyring_Authentication(T *testing.T) {
	T.Parallel()

	T.Run("mismatched associated data fails", func(t *testing.T) {
		t.Parallel()

		ring := twoKeyRing(t)

		ciphertext, err := ring.Encrypt(t.Context(), []byte("secret"), []byte("row-7"))
		must.NoError(t, err)

		// The transplant this prevents: the same ciphertext presented as
		// though it belonged to a different row.
		_, err = ring.Decrypt(t.Context(), ciphertext, []byte("row-8"))
		test.ErrorIs(t, err, ErrAuthenticationFailed)
	})

	T.Run("supplying associated data where none was bound fails", func(t *testing.T) {
		t.Parallel()

		ring := twoKeyRing(t)

		ciphertext, err := ring.Encrypt(t.Context(), []byte("secret"), nil)
		must.NoError(t, err)

		_, err = ring.Decrypt(t.Context(), ciphertext, []byte("row-7"))
		test.ErrorIs(t, err, ErrAuthenticationFailed)
	})

	T.Run("a rewritten key ID fails rather than redirecting", func(t *testing.T) {
		t.Parallel()

		ring := twoKeyRing(t)

		ciphertext, err := ring.Encrypt(t.Context(), []byte("secret"), []byte("row-7"))
		must.NoError(t, err)

		// Flip the frame's key ID from k2 to k1. Both keys are in the ring, so
		// dispatch succeeds and only the authenticated header stands in the
		// way — which is exactly why the header is authenticated.
		tampered := make([]byte, len(ciphertext))
		copy(tampered, ciphertext)
		tampered[3] = '1'

		_, err = ring.Decrypt(t.Context(), tampered, []byte("row-7"))
		test.ErrorIs(t, err, ErrAuthenticationFailed)
	})

	T.Run("a truncated ciphertext is malformed, not unauthenticated", func(t *testing.T) {
		t.Parallel()

		ring := twoKeyRing(t)

		_, err := ring.Decrypt(t.Context(), []byte{frameVersion}, nil)
		test.ErrorIs(t, err, ErrMalformedCiphertext)
	})
}
