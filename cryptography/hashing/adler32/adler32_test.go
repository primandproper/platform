package adler32

import (
	"testing"

	"github.com/primandproper/platform-go/v7/cryptography/hashing"

	"github.com/shoenig/test"
)

// Renaming this test changes t.Name() and therefore the expected digest, which
// is carried over unchanged from before Hasher returned raw bytes.
func Test_adler32Hasher_Hash(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		hasher := NewAdler32Hasher()

		test.EqOp(t, "c7060c2b", hashing.HexString(hasher, t.Name()))
	})

	T.Run("digest is four bytes wide", func(t *testing.T) {
		t.Parallel()

		test.SliceLen(t, 4, NewAdler32Hasher().Hash([]byte("anything")))
	})

	T.Run("empty and nil content agree", func(t *testing.T) {
		t.Parallel()

		hasher := NewAdler32Hasher()

		test.Eq(t, hasher.Hash(nil), hasher.Hash([]byte{}))
	})
}
