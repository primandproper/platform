package grpc

import (
	"context"
	stderrors "errors"
	"sync"

	"github.com/primandproper/platform-go/v14/cryptography/requestsigning"
	platformerrors "github.com/primandproper/platform-go/v14/errors"
	"github.com/primandproper/platform-go/v14/ratelimiting"

	"github.com/cockroachdb/errors/errorspb"
	gogoproto "github.com/gogo/protobuf/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/anypb"
)

const encodedErrorTypeURL = "type.googleapis.com/cockroach.errorspb.EncodedError"

// DecodeErrorFromStatus extracts the EncodedError from gRPC status details (if present)
// and decodes it so errors.Is() works across the wire. Returns the decoded error, or the
// original status error if no encoded detail is found.
func DecodeErrorFromStatus(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	st, ok := status.FromError(err)
	if !ok {
		return err
	}
	for _, detail := range st.Details() {
		if anyDetail, isAny := detail.(*anypb.Any); isAny && anyDetail != nil && anyDetail.TypeUrl == encodedErrorTypeURL {
			var enc errorspb.EncodedError
			if unmarshalErr := gogoproto.Unmarshal(anyDetail.Value, &enc); unmarshalErr != nil {
				continue
			}
			if decoded := platformerrors.DecodeError(ctx, enc); decoded != nil {
				return decoded
			}
		}
	}
	return err
}

// encodeErrorToDetails adds the platform-encoded error to status details for wire transmission.
// Uses gogo/protobuf for cockroachdb/errors EncodedError; wraps in anypb for gRPC compatibility.
func encodeErrorToDetails(ctx context.Context, err error) *anypb.Any {
	encoded := platformerrors.EncodeError(ctx, err)
	enc := &encoded
	if enc.GetLeaf() == nil && enc.GetWrapper() == nil {
		return nil
	}
	marshaled, marshalErr := gogoproto.Marshal(enc)
	if marshalErr != nil {
		return nil
	}
	return &anypb.Any{
		TypeUrl: encodedErrorTypeURL,
		Value:   marshaled,
	}
}

// clientMessage returns the status message a client sees.
//
// It is derived from the gRPC code, not from err.Error(). A handler error's text
// is the whole wrapped chain — table names, connection strings, the specific
// permission that was missing — and this package's own sentinels are documented
// as deliberately generic precisely because their message reaches clients
// verbatim. Putting arbitrary internal text on that same channel contradicted
// the stance the package states about itself.
//
// The full error still crosses the wire in the status *details*, encoded, which
// is what DecodeErrorFromStatus reads to keep errors.Is working between
// services. That detail is for trusted service-to-service callers; do not expose
// an interceptor-wrapped server directly to untrusted clients without stripping
// it at the edge.
func clientMessage(code codes.Code, err error) string {
	// Platform sentinels are written to be client-safe, so their own text is
	// better than a generic string — it tells the caller what to do differently.
	for _, sentinel := range clientSafeSentinels {
		if stderrors.Is(err, sentinel) {
			return sentinel.Error()
		}
	}

	registeredClientSafeMu.RLock()
	registered := registeredClientSafe
	registeredClientSafeMu.RUnlock()

	for _, sentinel := range registered {
		if stderrors.Is(err, sentinel) {
			return sentinel.Error()
		}
	}

	return code.String()
}

// clientSafeSentinels are the platform errors whose messages are documented as
// safe to return verbatim.
//
// It is the platform tier only, for the same reason PlatformMapper is: this
// package is a primitive and cannot import what is built on it. A domain whose
// sentinels are written to be read by a caller — links is the worked example,
// where four separate redemption outcomes exist precisely so that a person is
// told which one happened — hands them to RegisterClientSafeSentinels.
var clientSafeSentinels = []error{
	platformerrors.ErrPermissionDenied,
	ratelimiting.ErrRateLimited,
	// Both entitlement sentinels. Their messages name no feature and no limit —
	// "not entitled" and "quota exhausted" — and both codes they map to are
	// otherwise ambiguous enough that a client cannot tell which of two very
	// different remedies applies.
	platformerrors.ErrNotEntitled,
	platformerrors.ErrQuotaExhausted,
	// Both signature sentinels: neither says anything about the key, and the
	// stale one names clock skew, which is the difference between a caller that
	// can fix itself and one that files a ticket.
	requestsigning.ErrStaleSignature,
	requestsigning.ErrInvalidSignature,
	platformerrors.ErrNilInputParameter,
	platformerrors.ErrEmptyInputParameter,
	platformerrors.ErrInvalidIDProvided,
	platformerrors.ErrEmptyInputProvided,
	platformerrors.ErrUnrecognizedInputValue,
}

var (
	registeredClientSafe   []error
	registeredClientSafeMu sync.RWMutex
)

// RegisterClientSafeSentinels records errors whose own message the interceptors
// may put on the wire verbatim, rather than the generic string a gRPC code
// renders as.
//
// It is the companion to RegisterGRPCErrorMapper and answers the other half of
// the same question: the mapper decides the status, this decides whether the
// status carries the sentinel's own words. Registering a mapper without these
// is the usual case — most sentinels describe the system rather than the caller
// — and a sentinel whose text names a table, a key, or a policy must not be
// registered here at all.
//
// This module's own set is links.ClientSafeSentinels, and errormappers.Register
// hands it over alongside the four mappers, so a consumer registering the domain
// tier gets both halves in one call.
//
// It is additive and safe to call from more than one goroutine, and a sentinel
// registered twice costs a second comparison and nothing else.
func RegisterClientSafeSentinels(sentinels ...error) {
	registeredClientSafeMu.Lock()
	defer registeredClientSafeMu.Unlock()
	registeredClientSafe = append(registeredClientSafe, sentinels...)
}

// UnaryErrorEncodingInterceptor returns a unary interceptor that encodes handler
// errors into gRPC status details for wire transmission.
// Handlers should return errors (optionally wrapped); the interceptor will
// derive the gRPC code via MapToGRPC and attach the encoded error to details.
func UnaryErrorEncodingInterceptor() grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		resp, err := handler(ctx, req)
		if err == nil {
			return resp, nil
		}

		code := MapToGRPC(err, codes.Unknown)

		// An error the handler already shaped as a status carries a message the
		// handler chose to expose; anything else gets a code-derived one.
		msg := clientMessage(code, err)
		if st, ok := status.FromError(err); ok {
			code = MapToGRPC(err, st.Code())
			msg = st.Message()
		}

		st := status.New(code, msg)
		if detail := encodeErrorToDetails(ctx, err); detail != nil {
			if stWithDetails, withDetailsErr := st.WithDetails(detail); withDetailsErr == nil {
				st = stWithDetails
			}
		}
		return nil, st.Err()
	}
}

// StreamErrorEncodingInterceptor returns a stream interceptor that encodes
// handler errors into gRPC status details for wire transmission.
func StreamErrorEncodingInterceptor() grpc.StreamServerInterceptor {
	return func(
		srv any,
		ss grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		err := handler(srv, ss)
		if err == nil {
			return nil
		}

		code := MapToGRPC(err, codes.Unknown)

		// An error the handler already shaped as a status carries a message the
		// handler chose to expose; anything else gets a code-derived one.
		msg := clientMessage(code, err)
		if st, ok := status.FromError(err); ok {
			code = MapToGRPC(err, st.Code())
			msg = st.Message()
		}

		st := status.New(code, msg)
		if detail := encodeErrorToDetails(ss.Context(), err); detail != nil {
			if stWithDetails, withDetailsErr := st.WithDetails(detail); withDetailsErr == nil {
				st = stWithDetails
			}
		}
		return st.Err()
	}
}
