package random

import (
	"math/rand/v2"
)

// Element fetches a random element from an array.
func Element[T any](s []T) T {
	//nolint:gosec // not going to use crypto/rand for this
	return s[rand.IntN(len(s))]
}
