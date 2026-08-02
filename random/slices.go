package random

import (
	"crypto/rand"
	"math/big"
)

// Element fetches a random element from a slice. It returns the zero value of T
// for an empty slice rather than panicking.
//
// The choice comes from crypto/rand, not math/rand: this package is documented
// as cryptographically secure, and a caller reaching for it to pick a token, a
// shard, or a decoy from a slice has every reason to read that promise as
// covering this function too. crypto/rand.Int cannot fail on any platform Go
// supports, so the error it returns is discarded rather than widening this
// signature for a case that does not occur.
func Element[T any](s []T) T {
	if len(s) == 0 {
		var zero T

		return zero
	}

	n, err := rand.Int(rand.Reader, big.NewInt(int64(len(s))))
	if err != nil {
		return s[0]
	}

	return s[n.Int64()]
}
