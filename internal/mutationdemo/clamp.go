// Package mutationdemo exists to demonstrate what the mutation gate reports.
// It is not used by anything and is not meant to merge.
package mutationdemo

// Clamp confines v to the inclusive range [low, high].
func Clamp(v, low, high int) int {
	if v < low {
		return low
	}

	if v > high {
		return high
	}

	return v
}

// Offset reports the zero-based offset of a 1-based page, treating anything
// below the first page as the first page.
func Offset(page, size int) int {
	if page < 1 {
		page = 1
	}

	return (page - 1) * size
}
