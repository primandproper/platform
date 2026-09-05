package grpc

import (
	"database/sql"
	"errors"

	"github.com/primandproper/platform-go/v14/circuitbreaking"
	"github.com/primandproper/platform-go/v14/cryptography/requestsigning"
	"github.com/primandproper/platform-go/v14/database"
	platformerrors "github.com/primandproper/platform-go/v14/errors"
	"github.com/primandproper/platform-go/v14/idempotency"
	"github.com/primandproper/platform-go/v14/ratelimiting"
	textsearch "github.com/primandproper/platform-go/v14/search/text"
	vectorsearch "github.com/primandproper/platform-go/v14/search/vector"

	"google.golang.org/grpc/codes"
)

// PlatformMapper maps platform-level errors to gRPC codes.
// It does not depend on any domain.
//
// It covers the same sentinel set as the HTTP mapper, and deliberately so: a
// service exposing both transports would otherwise answer the same failure with
// a considered status on one and codes.Unknown on the other, and which one a
// client got would depend on how it happened to connect.
//
// "Platform" is a narrower word here than it looks. It means the primitives —
// database, circuitbreaking, ratelimiting, idempotency, requestsigning, the two
// search indexes, and the platformerrors sentinels — and nothing built on them.
// The mappings for dataprivacy, links, operations and sessions used to live in
// this switch and now live beside their own sentinels, as dataprivacy.GRPCMapper
// and its three counterparts, registered with RegisterGRPCErrorMapper. That is
// what lets this package be depended on by the tier it maps for instead of
// depending on it, and the both-transports parity above is now a property each
// of those four packages holds for its own sentinels.
//
// The practical consequence is that those four map only once somebody has
// registered them. service.Register does it for a service built from a
// service.Config; a service assembled by hand registers them itself.
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
	// The same code as a rate limit, and for the same reason: gRPC has one name
	// for "out of quota or rate", and a spent billing allowance is squarely
	// inside it. Checked before ErrNotEntitled, which it does not wrap but which
	// is the broader answer, so a caller that returns both does not collapse into
	// the vaguer one.
	case errors.Is(err, platformerrors.ErrQuotaExhausted):
		return codes.ResourceExhausted, true
	// PermissionDenied rather than FailedPrecondition, which was the other
	// candidate. gRPC's guidance is that PermissionDenied is for a caller that is
	// identified and may not do the thing — which is exactly true of an account
	// whose plan excludes a feature — while FailedPrecondition asks the client to
	// change system state, and paying for a subscription is not a state change
	// the RPC's client can perform. HTTP has 402 to be precise with; gRPC does
	// not, and inventing precision it lacks would only make the code unmappable
	// by a gateway.
	case errors.Is(err, platformerrors.ErrNotEntitled):
		return codes.PermissionDenied, true
	// Unauthenticated rather than PermissionDenied: gRPC's own guidance
	// separates "we do not know who you are" from "we know, and you may not".
	// A signature that does not verify is the first — nothing has been
	// identified yet, so there is nothing to deny.
	case errors.Is(err, requestsigning.ErrInvalidSignature),
		errors.Is(err, requestsigning.ErrStaleSignature):
		return codes.Unauthenticated, true
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
	// OutOfRange rather than InvalidArgument, which is the distinction gRPC's own
	// guidance draws between the two: InvalidArgument is an argument bad
	// regardless of the system's state, OutOfRange is one that ran past the end
	// of a range. A cursor at page 400 is well-formed and was well-formed when
	// the index issued it; it is only past the end. The alternatives are worse in
	// both directions — Internal turns a client-correctable refusal into a page
	// alert, and OK with an empty page tells the client it has seen everything.
	case errors.Is(err, textsearch.ErrResultWindowExceeded):
		return codes.OutOfRange, true
	// InvalidArgument for a cursor the index did not issue, which is the other
	// half of that distinction: nothing about the system's state makes it valid,
	// so it is bad input rather than exhausted range. Usually it is a cursor
	// carried over from a database-backed page — the two kinds share a field —
	// or one left over from a backend swap.
	case errors.Is(err, textsearch.ErrInvalidCursor),
		errors.Is(err, textsearch.ErrEmptyQueryProvided):
		return codes.InvalidArgument, true
	// The vector index's request-shaped refusals: a missing vector is a NotFound
	// like any other, and the other two are queries the index cannot evaluate.
	// Its construction-time sentinels are deliberately unmapped, for the reason
	// the HTTP mapper spells out where it does the same.
	case errors.Is(err, vectorsearch.ErrNotFound):
		return codes.NotFound, true
	case errors.Is(err, vectorsearch.ErrEmptyEmbedding),
		errors.Is(err, vectorsearch.ErrDimensionMismatch):
		return codes.InvalidArgument, true
	case errors.Is(err, platformerrors.ErrNilInputParameter),
		errors.Is(err, platformerrors.ErrEmptyInputParameter),
		errors.Is(err, platformerrors.ErrInvalidIDProvided),
		errors.Is(err, platformerrors.ErrEmptyInputProvided),
		errors.Is(err, platformerrors.ErrUnrecognizedInputValue):
		return codes.InvalidArgument, true
	default:
		return codes.Unknown, false
	}
}
