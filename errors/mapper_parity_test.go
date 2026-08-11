package errors_test

import (
	"testing"

	"github.com/primandproper/platform-go/v10/circuitbreaking"
	"github.com/primandproper/platform-go/v10/cryptography/requestsigning"
	"github.com/primandproper/platform-go/v10/database"
	"github.com/primandproper/platform-go/v10/dataprivacy"
	platformerrors "github.com/primandproper/platform-go/v10/errors"
	grpcerrors "github.com/primandproper/platform-go/v10/errors/grpc"
	httperrors "github.com/primandproper/platform-go/v10/errors/http"
	"github.com/primandproper/platform-go/v10/idempotency"
	"github.com/primandproper/platform-go/v10/links"
	"github.com/primandproper/platform-go/v10/operations"
	"github.com/primandproper/platform-go/v10/ratelimiting"
	"github.com/primandproper/platform-go/v10/sessions"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
	"google.golang.org/grpc/codes"
)

// mappedSentinels is every platform sentinel both transports are expected to
// have an answer for.
//
// It is a single list rather than one per mapper on purpose. The two drifted
// apart once already — gRPC had no case for sessions or operations, so an
// expired session reached an HTTP client as a considered 401 and a gRPC client
// as codes.Unknown — and a per-mapper list is exactly the shape that lets it
// happen again without anybody noticing.
//
// Adding a sentinel here and to only one mapper fails this test, which is the
// point. A sentinel that genuinely belongs on one transport and not the other
// does not go in this list; it goes in that mapper's own test, with a comment
// saying why.
var mappedSentinels = []error{
	database.ErrUserAlreadyExists,
	circuitbreaking.ErrCircuitBroken,
	platformerrors.ErrPermissionDenied,
	platformerrors.ErrResourceInUse,
	platformerrors.ErrQuotaExhausted,
	platformerrors.ErrNotEntitled,
	platformerrors.ErrNilInputParameter,
	platformerrors.ErrEmptyInputParameter,
	platformerrors.ErrInvalidIDProvided,
	platformerrors.ErrEmptyInputProvided,
	platformerrors.ErrUnrecognizedInputValue,
	ratelimiting.ErrRateLimited,
	requestsigning.ErrStaleSignature,
	requestsigning.ErrInvalidSignature,
	sessions.ErrNotFound,
	sessions.ErrExpired,
	sessions.ErrIdleTimeout,
	sessions.ErrAbsoluteTimeout,
	links.ErrLinkNotFound,
	links.ErrLinkAlreadyRedeemed,
	links.ErrLinkExpired,
	links.ErrLinkRevoked,
	links.ErrInvalidToken,
	operations.ErrOperationNotFound,
	operations.ErrTooManyWatchers,
	dataprivacy.ErrRequestNotFound,
	dataprivacy.ErrNotAwaitingConfirmation,
	dataprivacy.ErrArtifactUnavailable,
	dataprivacy.ErrEmptySubjectID,
	dataprivacy.ErrUnknownRequestType,
	idempotency.ErrInFlight,
	idempotency.ErrFingerprintMismatch,
	idempotency.ErrKeyRequired,
	idempotency.ErrKeyTooLong,
	idempotency.ErrKeyInvalid,
}

func TestPlatformMappers_coverTheSameSentinels(T *testing.T) {
	T.Parallel()

	for _, sentinel := range mappedSentinels {
		T.Run(sentinel.Error(), func(t *testing.T) {
			t.Parallel()

			code, msg, httpOK := httperrors.PlatformMapper.Map(sentinel)
			test.True(t, httpOK, test.Sprintf("no HTTP mapping for %v", sentinel))
			test.NotEq(t, httperrors.ErrorCode(""), code)
			test.NotEq(t, "", msg)

			grpcCode, grpcOK := grpcerrors.PlatformMapper.Map(sentinel)
			test.True(t, grpcOK, test.Sprintf("no gRPC mapping for %v", sentinel))
			test.NotEqOp(t, codes.Unknown, grpcCode)
		})
	}
}

func TestPlatformMappers_wrappedSentinelsStillMap(T *testing.T) {
	T.Parallel()

	// The mappers are reached from handlers, which wrap. A mapping that only
	// works on a bare sentinel works nowhere real.
	for _, sentinel := range mappedSentinels {
		T.Run(sentinel.Error(), func(t *testing.T) {
			t.Parallel()

			wrapped := platformerrors.Wrap(sentinel, "doing the thing")

			_, _, httpOK := httperrors.PlatformMapper.Map(wrapped)
			test.True(t, httpOK)

			_, grpcOK := grpcerrors.PlatformMapper.Map(wrapped)
			test.True(t, grpcOK)
		})
	}
}

func TestPlatformMappers_unknownErrorsAreNotClaimed(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		stranger := platformerrors.New("something nobody has a mapping for")

		_, _, httpOK := httperrors.PlatformMapper.Map(stranger)
		test.False(t, httpOK)

		grpcCode, grpcOK := grpcerrors.PlatformMapper.Map(stranger)
		test.False(t, grpcOK)
		test.EqOp(t, codes.Unknown, grpcCode)
	})
}

func TestPlatformMappers_dataprivacyIsMappedInBoth(T *testing.T) {
	T.Parallel()

	// Called out separately because it was the gap: a subject asking after their
	// own export got a 500 saying the service was broken, when the answer was
	// that the ID was not one of theirs.
	T.Run("a missing request is a not-found, not a server failure", func(t *testing.T) {
		t.Parallel()

		code, _, ok := httperrors.PlatformMapper.Map(dataprivacy.ErrRequestNotFound)
		must.True(t, ok)
		test.EqOp(t, httperrors.ErrDataNotFound, code)

		grpcCode, grpcOK := grpcerrors.PlatformMapper.Map(dataprivacy.ErrRequestNotFound)
		must.True(t, grpcOK)
		test.EqOp(t, codes.NotFound, grpcCode)
	})
}
