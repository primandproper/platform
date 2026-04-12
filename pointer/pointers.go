package pointer

// To returns a pointer to a value.
//
//go:fix inline
func To[T any](x T) *T {
	return new(x)
}

// ToSlice returns the value of a pointer for every element in a slice.
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
		y[i] = *x[i]
	}
	return y
}
