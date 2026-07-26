package hashing

import (
	"testing"

	"github.com/shoenig/test"
)

func TestHasherInterfaceExists(T *testing.T) {
	T.Parallel()

	T.Run("interface is satisfiable", func(t *testing.T) {
		t.Parallel()

		var _ Hasher = (*mockHasher)(nil)
	})
}

func TestHex(T *testing.T) {
	T.Parallel()

	T.Run("hex-encodes the raw digest", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, "00ff10", Hex(&mockHasher{digest: []byte{0x00, 0xff, 0x10}}, nil))
	})

	T.Run("empty digest encodes to the empty string", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, "", Hex(&mockHasher{}, nil))
	})
}

func TestHexString(T *testing.T) {
	T.Parallel()

	T.Run("passes the string through as bytes", func(t *testing.T) {
		t.Parallel()

		h := &mockHasher{digest: []byte{0xab}}

		test.EqOp(t, "ab", HexString(h, "content"))
		test.EqOp(t, "content", string(h.lastContent))
	})
}

// mockHasher returns a fixed digest and records what it was asked to hash.
type mockHasher struct {
	digest      []byte
	lastContent []byte
}

func (m *mockHasher) Hash(content []byte) []byte {
	m.lastContent = content

	return m.digest
}
