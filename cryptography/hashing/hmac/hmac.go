package hmac

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"hash"

	"github.com/primandproper/platform-go/v8/cryptography/hashing"
)

var _ hashing.Hasher = (*hmacHasher)(nil)

// hmacHasher is a hashing.Hasher that authenticates rather than merely digests:
// its output depends on a key fixed at construction as well as on the content.
type hmacHasher struct {
	newHash func() hash.Hash
	key     []byte
}

// NewHMACSHA256Hasher returns a hashing.Hasher computing HMAC-SHA-256 under key.
// The key is copied, so a caller may reuse or zero its buffer afterwards.
//
// An empty key is accepted because crypto/hmac accepts one, and rejecting it
// here would put an error return on a constructor that has nothing else to fail
// on. It is not a meaningful MAC — callers deriving keys from configuration
// should check for emptiness themselves.
func NewHMACSHA256Hasher(key []byte) hashing.Hasher {
	return newHasher(key, sha256.New)
}

// NewHMACSHA512Hasher returns a hashing.Hasher computing HMAC-SHA-512 under key,
// on the same terms as NewHMACSHA256Hasher.
func NewHMACSHA512Hasher(key []byte) hashing.Hasher {
	return newHasher(key, sha512.New)
}

func newHasher(key []byte, newHash func() hash.Hash) *hmacHasher {
	return &hmacHasher{
		newHash: newHash,
		key:     append([]byte(nil), key...),
	}
}

// Hash returns the MAC of content under the hasher's key.
//
// A fresh hash.Hash is allocated per call rather than reset and reused, which
// is what makes the hasher safe to share across goroutines — a delivery worker
// signs concurrently from one endpoint's hasher, and shared internal state
// would interleave two payloads into one signature.
func (h *hmacHasher) Hash(content []byte) []byte {
	mac := hmac.New(h.newHash, h.key)

	// hash.Hash documents Write as never returning an error, which is what lets
	// hashing.Hasher have no error return at all.
	mac.Write(content)

	return mac.Sum(nil)
}

// Equal reports whether two MACs are identical, in time independent of how far
// they agree.
//
// It exists because the natural comparison is the wrong one. A digest is
// routinely rendered with hashing.Hex and compared as a string, and == on
// strings returns at the first differing byte — which, repeated against an
// attacker-controlled candidate, leaks the expected MAC a byte at a time. Verify
// paths must compare raw Hash output through this, never hex through ==.
func Equal(a, b []byte) bool {
	return hmac.Equal(a, b)
}
