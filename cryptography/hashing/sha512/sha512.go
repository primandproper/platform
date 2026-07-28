package sha512

import (
	"crypto/sha512"

	"github.com/primandproper/platform-go/v8/cryptography/hashing"
)

var _ hashing.Hasher = (*sha512Hasher)(nil)

type (
	sha512Hasher struct{}
)

// NewSHA512Hasher returns a hashing.Hasher backed by SHA-512. Code that does
// not need the hashing.Hasher seam should call crypto/sha512 directly; this
// exists so a digest algorithm can be selected at runtime.
func NewSHA512Hasher() hashing.Hasher {
	return &sha512Hasher{}
}

func (s *sha512Hasher) Hash(content []byte) []byte {
	sum := sha512.Sum512(content)

	return sum[:]
}
