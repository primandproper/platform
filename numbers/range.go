package numbers

import (
	"context"

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

	// OpenRangeUpdateRequestInput represents an update request for an open range.
	OpenRangeUpdateRequestInput[T Numeric] struct {
		Min *T `json:"min,omitempty"`
		Max *T `json:"max,omitempty"`
	}
)

var _ validation.ValidatableWithContext = (*MinRange[int])(nil)

func (x *MinRange[T]) ValidateWithContext(ctx context.Context) error {
	return validation.ValidateStructWithContext(
		ctx,
		x,
		validation.Field(&x.Min, validation.Required),
	)
}
