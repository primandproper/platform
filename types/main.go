package types

import (
	"context"
	"fmt"

	"github.com/primandproper/platform/database/filtering"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

type (
	// ResponseDetails represents details about the response.
	ResponseDetails struct {
		_ struct{} `json:"-"`

		CurrentAccountID string `json:"currentAccountID"`
		TraceID          string `json:"traceID"`
	}

	// APIResponse represents a response we might send to the user.
	APIResponse[T any] struct {
		_ struct{} `json:"-"`

		Data       T                     `json:"data,omitempty"`
		Pagination *filtering.Pagination `json:"pagination,omitempty"`
		Error      *APIError             `json:"error,omitempty"`
		Details    ResponseDetails       `json:"details"`
	}

	// APIError represents a response we might send to the User in the event of an error.
	APIError struct {
		_ struct{} `json:"-"`

		Message string    `json:"message"`
		Code    ErrorCode `json:"code"`
	}

	// NamedID pairs an identifier with a human-readable name.
	NamedID struct {
		_ struct{} `json:"-"`

		ID   string `json:"id,omitempty"`
		Name string `json:"name,omitempty"`
	}

	// RangeWithOptionalUpperBound represents a range with a required minimum and optional maximum.
	RangeWithOptionalUpperBound[T comparable] struct {
		Min T  `json:"min"`
		Max *T `json:"max,omitempty"`
	}

	// OptionalRange represents a range where both minimum and maximum are optional.
	OptionalRange[T comparable] struct {
		Min *T `json:"min,omitempty"`
		Max *T `json:"max,omitempty"`
	}

	// OptionalRangeUpdateRequestInput represents an update request for an optional range.
	OptionalRangeUpdateRequestInput[T comparable] struct {
		Min *T `json:"min,omitempty"`
		Max *T `json:"max,omitempty"`
	}

	// OptionalFloat32Range should be replaced with a generic Range type.
	OptionalFloat32Range OptionalRange[float32]

	// Float32RangeWithOptionalMax should be replaced with a generic Range type.
	Float32RangeWithOptionalMax RangeWithOptionalUpperBound[float32]

	// Float32RangeWithOptionalMaxUpdateRequestInput should be replaced with a generic Range type.
	Float32RangeWithOptionalMaxUpdateRequestInput OptionalRangeUpdateRequestInput[float32]

	// Uint16RangeWithOptionalMax should be replaced with a generic Range type.
	Uint16RangeWithOptionalMax RangeWithOptionalUpperBound[uint16]

	// Uint16RangeWithOptionalMaxUpdateRequestInput should be replaced with a generic Range type.
	Uint16RangeWithOptionalMaxUpdateRequestInput OptionalRangeUpdateRequestInput[uint16]

	// OptionalUint32Range should be replaced with a generic Range type.
	OptionalUint32Range OptionalRange[uint32]

	// Uint32RangeWithOptionalMax should be replaced with a generic Range type.
	Uint32RangeWithOptionalMax RangeWithOptionalUpperBound[uint32]

	// Uint32RangeWithOptionalMaxUpdateRequestInput should be replaced with a generic Range type.
	Uint32RangeWithOptionalMaxUpdateRequestInput OptionalRangeUpdateRequestInput[uint32]
)

// Error returns the error message.
func (e *APIError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// AsError returns the error message.
func (e *APIError) AsError() error {
	if e == nil {
		return nil
	}
	return e
}

// NewAPIErrorResponse returns a new APIResponse with an error field.
func NewAPIErrorResponse(issue string, code ErrorCode, details ResponseDetails) *APIResponse[any] {
	return &APIResponse[any]{
		Error: &APIError{
			Message: issue,
			Code:    code,
		},
		Details: details,
	}
}

var _ validation.ValidatableWithContext = (*Float32RangeWithOptionalMax)(nil)

func (x *Float32RangeWithOptionalMax) ValidateWithContext(ctx context.Context) error {
	return validation.ValidateStructWithContext(
		ctx,
		x,
		validation.Field(&x.Min, validation.Required),
	)
}

var _ validation.ValidatableWithContext = (*Uint16RangeWithOptionalMax)(nil)

func (x *Uint16RangeWithOptionalMax) ValidateWithContext(ctx context.Context) error {
	return validation.ValidateStructWithContext(
		ctx,
		x,
		validation.Field(&x.Min, validation.Required),
	)
}

var _ validation.ValidatableWithContext = (*Uint32RangeWithOptionalMax)(nil)

func (x *Uint32RangeWithOptionalMax) ValidateWithContext(ctx context.Context) error {
	return validation.ValidateStructWithContext(
		ctx,
		x,
		validation.Field(&x.Min, validation.Required),
	)
}

var _ validation.ValidatableWithContext = (*RangeWithOptionalUpperBound[string])(nil)

func (x *RangeWithOptionalUpperBound[T]) ValidateWithContext(ctx context.Context) error {
	return validation.ValidateStructWithContext(
		ctx,
		x,
		validation.Field(&x.Min, validation.Required),
	)
}
