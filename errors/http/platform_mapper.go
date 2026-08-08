package http

import (
	"database/sql"
	"errors"

	"github.com/primandproper/platform-go/v10/circuitbreaking"
	"github.com/primandproper/platform-go/v10/database"
	platformerrors "github.com/primandproper/platform-go/v10/errors"
	"github.com/primandproper/platform-go/v10/idempotency"
	"github.com/primandproper/platform-go/v10/ratelimiting"
)

// PlatformMapper maps platform-level errors to HTTP error codes and messages.
// It does not depend on any domain.
var PlatformMapper HTTPErrorMapper = platformMapper{}

type platformMapper struct{}

func (platformMapper) Map(err error) (code ErrorCode, msg string, ok bool) {
	if err == nil {
		return ErrNothingSpecific, "", false
	}
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return ErrDataNotFound, "data not found", true
	case errors.Is(err, database.ErrUserAlreadyExists):
		return ErrValidatingRequestInput, "user already exists", true
	case errors.Is(err, circuitbreaking.ErrCircuitBroken):
		return ErrCircuitBroken, "service temporarily unavailable", true
	case errors.Is(err, platformerrors.ErrPermissionDenied):
		// The message is a constant rather than anything derived from the error:
		// naming the missing permission would disclose the permission taxonomy to
		// a caller that just failed to authorize.
		return ErrUserIsNotAuthorized, "permission denied", true
	case errors.Is(err, platformerrors.ErrResourceInUse):
		return ErrResourceConflict, "resource is in use", true
	case errors.Is(err, ratelimiting.ErrRateLimited):
		// The message says nothing about the limit or the key it was counted
		// against. Both are useful to an operator and useful to an attacker
		// probing for the threshold; when a limiter can say when to come back,
		// that answer belongs in Retry-After, where a client can act on it.
		return ErrTooManyRequests, "too many requests", true
	case errors.Is(err, idempotency.ErrInFlight):
		return ErrIdempotencyKeyInFlight, "a request with this idempotency key is already in progress", true
	case errors.Is(err, idempotency.ErrFingerprintMismatch):
		return ErrIdempotencyKeyReused, "this idempotency key was already used for a different request", true
	// A malformed key is ordinary bad input, not an idempotency outcome, so it
	// gets the input code and a 400 rather than one of the codes above.
	case errors.Is(err, idempotency.ErrKeyRequired),
		errors.Is(err, idempotency.ErrKeyTooLong),
		errors.Is(err, idempotency.ErrKeyInvalid):
		return ErrValidatingRequestInput, "invalid idempotency key", true
	case errors.Is(err, platformerrors.ErrNilInputParameter),
		errors.Is(err, platformerrors.ErrEmptyInputParameter),
		errors.Is(err, platformerrors.ErrNilInputProvided),
		errors.Is(err, platformerrors.ErrInvalidIDProvided),
		errors.Is(err, platformerrors.ErrEmptyInputProvided):
		return ErrValidatingRequestInput, "invalid input", true
	default:
		return "", "", false
	}
}
