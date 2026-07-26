package sha256

import (
	"testing"

	"github.com/primandproper/platform-go/v7/cryptography/hashing"

	"github.com/shoenig/test"
)

// Renaming this test changes t.Name() and therefore the expected digest, which
// is carried over unchanged from before Hasher returned raw bytes.
func Test_sha256Hasher_Hash(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		hasher := NewSHA256Hasher()

		test.EqOp(t, "f469799cfc8eb5c3fa03e2ec4faf3c1b9a4c3a1c0ac3557a2f963e598cea695f", hashing.HexString(hasher, t.Name()))
	})

	T.Run("digest is thirty-two bytes wide", func(t *testing.T) {
		t.Parallel()

		test.SliceLen(t, 32, NewSHA256Hasher().Hash([]byte("anything")))
	})
}
