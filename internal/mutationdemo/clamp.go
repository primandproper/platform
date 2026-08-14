// Package mutationdemo exists to demonstrate what the mutation gate reports.
// It is not used by anything and is not meant to merge.
package mutationdemo

// Clamp confines v to the inclusive range [low, high].
//
// Both of its comparisons carry an equivalent mutant: at v == low, returning
// low and returning v are the same value, so `<` and `<=` cannot be told apart
// through the result. No test can kill those, which is why the gate reports
// survivors instead of failing on them.
func Clamp(v, low, high int) int {
	if v < low {
		return low
	}

	if v > high {
		return high
	}

	return v
}

// Bucket labels n against a threshold it does not itself reach.
//
// Unlike Clamp, this boundary is observable: at n == threshold the two sides
// return different strings, so an assertion at the edge kills the mutant.
func Bucket(n, threshold int) string {
	if n < threshold {
		return "small"
	}

	return "large"
}

// Offset reports the zero-based offset of a 1-based page, treating anything
// below the first page as the first page.
func Offset(page, size int) int {
	if page < 1 {
		page = 1
	}

	return (page - 1) * size
}
