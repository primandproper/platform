package sha256

import (
	"crypto/sha256"

	"github.com/primandproper/platform-go/v10/cryptography/hashing"
)

var _ hashing.Hasher = (*Hasher)(nil)

// Hasher is the SHA-256 hashing.Hasher implementation. It is exported, and
// returned by NewSHA256Hasher, so a caller who has chosen SHA-256 can depend on
// that choice rather than on the interface every digest algorithm shares.
type (
	Hasher struct{}
)

// NewSHA256Hasher returns a hashing.Hasher backed by SHA-256. Code that does
// not need the hashing.Hasher seam should call crypto/sha256 directly; this
// exists so a digest algorithm can be selected at runtime.
func NewSHA256Hasher() *Hasher {
	return &Hasher{}
}

func (s *Hasher) Hash(content []byte) []byte {
	sum := sha256.Sum256(content)

	return sum[:]
}
