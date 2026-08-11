package numbers

import (
	"context"
	"errors"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

// Numeric is a constraint for all built-in numeric types.
type Numeric interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 |
		~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr |
		~float32 | ~float64
}

type (
	// MinRange represents a range with a required minimum and optional maximum.
	MinRange[T Numeric] struct {
		Min T  `json:"min"`
		Max *T `json:"max,omitempty"`
	}

	// OpenRange represents a range where both minimum and maximum are optional.
	OpenRange[T Numeric] struct {
		Min *T `json:"min,omitempty"`
		Max *T `json:"max,omitempty"`
	}
)

var (
	_ validation.ValidatableWithContext = (*MinRange[int])(nil)
	_ validation.ValidatableWithContext = (*OpenRange[int])(nil)
)

func (x *MinRange[T]) ValidateWithContext(ctx context.Context) error {
	// Min is a value type that is always present, so `Required` (which rejects the
	// zero value) would wrongly reject a legitimate range starting at 0. Instead only
	// enforce that Max, when set, is not below Min.
	return validation.ValidateStructWithContext(
		ctx,
		x,
		validation.Field(&x.Max, validation.By(x.validateMaxNotBelowMin)),
	)
}

func (x *MinRange[T]) validateMaxNotBelowMin(any) error {
	if x.Max != nil && *x.Max < x.Min {
		return errors.New("max must be greater than or equal to min")
	}
	return nil
}

// ValidateWithContext enforces the one thing an OpenRange can get wrong.
//
// Both bounds are optional, so neither is Required and an entirely empty range
// — "no bound either way" — is valid. What is not valid is the same inversion
// MinRange refuses: a maximum below the minimum describes a range no value can
// satisfy, and a filter built from one silently returns nothing rather than
// reporting that it was asked for the impossible. Absence of either bound has
// nothing to compare, and passes.
func (x *OpenRange[T]) ValidateWithContext(ctx context.Context) error {
	return validation.ValidateStructWithContext(
		ctx,
		x,
		validation.Field(&x.Max, validation.By(x.validateMaxNotBelowMin)),
	)
}

func (x *OpenRange[T]) validateMaxNotBelowMin(any) error {
	if x.Min != nil && x.Max != nil && *x.Max < *x.Min {
		return errors.New("max must be greater than or equal to min")
	}
	return nil
}
