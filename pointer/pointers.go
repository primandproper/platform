package pointer

// To returns a pointer to a value.
//
// It carries no //go:fix inline directive. The body is exactly what a caller
// would write by hand now that new takes a value, so one is tempting, but no
// tool can apply it: inlining a call to a generic function means inferring T,
// which the inliner does not do. All the directive produced was a diagnostic at
// every call site, and an exclusion in .golangci.yml for the directive itself.
func To[T any](x T) *T {
	return new(x)
}

// ToSlice returns a pointer to each element in a slice.
func ToSlice[T any](x []T) []*T {
	if x == nil {
		return []*T{}
	}

	y := make([]*T, len(x))
	for i := range x {
		y[i] = new(x[i])
	}
	return y
}

// Dereference returns the value of a pointer.
func Dereference[T any](x *T) T {
	if x == nil {
		var zero T
		return zero
	}
	return *x
}

// DereferenceSlice returns the value of a pointer for every element in a slice.
func DereferenceSlice[T any](x []*T) []T {
	if x == nil {
		return []T{}
	}

	y := make([]T, len(x))
	for i := range x {
		if x[i] == nil {
			// Zero-fill a nil element rather than panicking on the deref, matching the
			// scalar Dereference helper — a []*T is exactly where nil elements show up.
			var zero T
			y[i] = zero
			continue
		}
		y[i] = *x[i]
	}
	return y
}
