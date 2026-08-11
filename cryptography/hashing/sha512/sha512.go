package sha512

import (
	"crypto/sha512"

	"github.com/primandproper/platform-go/v10/cryptography/hashing"
)

var _ hashing.Hasher = (*Hasher)(nil)

// Hasher is the SHA-512 hashing.Hasher implementation. It is exported, and
// returned by NewSHA512Hasher, so a caller who has chosen SHA-512 can depend on
// that choice rather than on the interface every digest algorithm shares.
type (
	Hasher struct{}
)

// NewSHA512Hasher returns a hashing.Hasher backed by SHA-512. Code that does
// not need the hashing.Hasher seam should call crypto/sha512 directly; this
// exists so a digest algorithm can be selected at runtime.
func NewSHA512Hasher() *Hasher {
	return &Hasher{}
}

func (s *Hasher) Hash(content []byte) []byte {
	sum := sha512.Sum512(content)

	return sum[:]
}
