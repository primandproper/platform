package grpc

import (
	"database/sql"
	"errors"

	"github.com/primandproper/platform-go/v10/circuitbreaking"
	"github.com/primandproper/platform-go/v10/database"
	platformerrors "github.com/primandproper/platform-go/v10/errors"
	"github.com/primandproper/platform-go/v10/idempotency"
	"github.com/primandproper/platform-go/v10/ratelimiting"

	"google.golang.org/grpc/codes"
)

// PlatformMapper maps platform-level errors to gRPC codes.
// It does not depend on any domain.
var PlatformMapper GRPCErrorMapper = platformMapper{}

type platformMapper struct{}

func (platformMapper) Map(err error) (code codes.Code, ok bool) {
	if err == nil {
		return codes.OK, false
	}
	switch {
	case errors.Is(err, database.ErrUserAlreadyExists):
		return codes.AlreadyExists, true
	case errors.Is(err, sql.ErrNoRows):
		return codes.NotFound, true
	case errors.Is(err, circuitbreaking.ErrCircuitBroken):
		return codes.Unavailable, true
	case errors.Is(err, platformerrors.ErrPermissionDenied):
		return codes.PermissionDenied, true
	// FailedPrecondition rather than Aborted: gRPC's own guidance gives
	// "rmdir on a non-empty directory" as the canonical FailedPrecondition
	// case, and a delete blocked by a live reference is the same shape. The
	// client must change the system state before retrying, not just retry.
	case errors.Is(err, platformerrors.ErrResourceInUse):
		return codes.FailedPrecondition, true
	// ResourceExhausted is gRPC's own name for "out of quota or rate", and its
	// documented client advice — back off and retry — is the right one here.
	// Unavailable would be wrong: nothing is down, and it invites the client to
	// fail over to another instance that shares the same limiter.
	case errors.Is(err, ratelimiting.ErrRateLimited):
		return codes.ResourceExhausted, true
	// Aborted is gRPC's concurrency-conflict code, and its documented advice —
	// retry at a higher level — is exactly right here: the work may still
	// succeed, and the client should ask again with the same key.
	case errors.Is(err, idempotency.ErrInFlight):
		return codes.Aborted, true
	case errors.Is(err, idempotency.ErrFingerprintMismatch),
		errors.Is(err, idempotency.ErrKeyRequired),
		errors.Is(err, idempotency.ErrKeyTooLong),
		errors.Is(err, idempotency.ErrKeyInvalid):
		return codes.InvalidArgument, true
	case errors.Is(err, platformerrors.ErrNilInputParameter),
		errors.Is(err, platformerrors.ErrEmptyInputParameter),
		errors.Is(err, platformerrors.ErrNilInputProvided),
		errors.Is(err, platformerrors.ErrInvalidIDProvided),
		errors.Is(err, platformerrors.ErrEmptyInputProvided):
		return codes.InvalidArgument, true
	default:
		return codes.Unknown, false
	}
}
