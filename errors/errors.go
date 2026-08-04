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

	// ErrResourceInUse indicates the request conflicts with the current state of
	// the resource — most often a delete of something another record still
	// references. It is a client-correctable conflict, not a server failure: the
	// same request may succeed once the references are gone.
	//
	// It lives here rather than in a data-access package for the same reason
	// ErrPermissionDenied does: errors/http and errors/grpc map it, and neither
	// may import a package that imports them back.
	ErrResourceInUse = crdberrors.New("resource is in use")

	// ErrUnknownProvider indicates a config named a provider the package does
	// not implement — a typo, or a provider from a newer version of this module.
	//
	// It lives here so that every config subpackage reports the same failure the
	// same way, and so a consumer's startup path can branch on one sentinel
	// rather than on a dozen package-local ones. Constructors wrap it with the
	// offending value; they never substitute a noop implementation, because a
	// misconfigured provider that silently discards its work is a production
	// incident that looks like a healthy process.
	ErrUnknownProvider = crdberrors.New("unknown provider")
)
