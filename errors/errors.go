package errors

import (
	crdberrors "github.com/cockroachdb/errors"
)

// Re-exports from cockroachdb/errors for construction and wrapping.
// Use std "errors" for Is, As, Unwrap - they work with these types.
var (
	New    = crdberrors.New
	Newf   = crdberrors.Newf
	Errorf = crdberrors.Errorf
	Wrap   = crdberrors.Wrap
	Wrapf  = crdberrors.Wrapf
	Join   = crdberrors.Join

	EncodeError = crdberrors.EncodeError
	DecodeError = crdberrors.DecodeError
)

// Common platform sentinels (wire-transmittable via cockroachdb/errors).
var (
	// ErrNilInputParameter is returned when an input parameter is nil.
	ErrNilInputParameter = crdberrors.New("provided input parameter is nil")
	// ErrEmptyInputParameter is returned when an input parameter is empty.
	ErrEmptyInputParameter = crdberrors.New("provided input parameter is empty")

	// ErrNilInputProvided indicates nil input was provided in an unacceptable context.
	ErrNilInputProvided = crdberrors.New("nil input provided")
	// ErrInvalidIDProvided indicates a required ID was passed in empty.
	ErrInvalidIDProvided = crdberrors.New("required ID provided is empty")
	// ErrEmptyInputProvided indicates a required input was passed in empty.
	ErrEmptyInputProvided = crdberrors.New("input provided is empty")

	// ErrPermissionDenied indicates the requester lacks the authority to perform
	// the action. It lives here rather than in the authorization package so that
	// errors/http and errors/grpc can map it without importing authorization,
	// which imports them back.
	//
	// Its message is deliberately generic: it reaches clients verbatim, and the
	// specific permission that was missing must not.
	ErrPermissionDenied = crdberrors.New("permission denied")
)
