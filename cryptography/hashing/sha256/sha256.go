package sha256

import (
	"crypto/sha256"

	"github.com/primandproper/platform-go/v8/cryptography/hashing"
)

var _ hashing.Hasher = (*sha256Hasher)(nil)

type (
	sha256Hasher struct{}
)

// NewSHA256Hasher returns a hashing.Hasher backed by SHA-256. Code that does
// not need the hashing.Hasher seam should call crypto/sha256 directly; this
// exists so a digest algorithm can be selected at runtime.
func NewSHA256Hasher() hashing.Hasher {
	return &sha256Hasher{}
}

func (s *sha256Hasher) Hash(content []byte) []byte {
	sum := sha256.Sum256(content)

	return sum[:]
}
