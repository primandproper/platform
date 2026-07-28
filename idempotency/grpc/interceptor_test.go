package grpc

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/primandproper/platform-go/v8/idempotency"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func TestNewUnaryServerInterceptor(T *testing.T) {
	T.Parallel()

	T.Run("rejects a nil manager", func(t *testing.T) {
		t.Parallel()

		_, err := NewUnaryServerInterceptor(nil)
		test.ErrorIs(t, err, ErrNilManager)
	})
}

func TestInterceptor_PassThrough(T *testing.T) {
	T.Parallel()

	T.Run("a call without the key is untouched", func(t *testing.T) {
		t.Parallel()

		handler := newCountingHandler(str("ok"))
		interceptor := newTestInterceptor(t)

		for range 2 {
			_, err := interceptor(t.Context(), str("req"), info(), handler.handle)
			must.NoError(t, err)
		}

		test.EqOp(t, int64(2), handler.Calls())
	})

	T.Run("a filtered-out method is untouched", func(t *testing.T) {
		t.Parallel()

		handler := newCountingHandler(str("ok"))
		interceptor := newTestInterceptor(t, WithMethodFilter(func(m string) bool {
			return strings.HasSuffix(m, "/Delete")
		}))

		ctx := keyed(t.Context(), testKey)
		for range 2 {
			_, err := interceptor(ctx, str("req"), info(), handler.handle)
			must.NoError(t, err)
		}

		test.EqOp(t, int64(2), handler.Calls())
	})

	// grpc-go permits non-proto codecs. Such a call cannot be fingerprinted,
	// and refusing it would break a service that never asked for any of this.
	T.Run("a non-proto request runs unguarded", func(t *testing.T) {
		t.Parallel()

		handler := newCountingHandler("plain string reply")
		interceptor := newTestInterceptor(t)

		ctx := keyed(t.Context(), testKey)
		for range 2 {
			_, err := interceptor(ctx, "plain string request", info(), handler.handle)
			must.NoError(t, err)
		}

		test.EqOp(t, int64(2), handler.Calls())
	})

	T.Run("a non-proto reply runs unguarded", func(t *testing.T) {
		t.Parallel()

		handler := newCountingHandler("plain string reply")
		interceptor := newTestInterceptor(t)

		ctx := keyed(t.Context(), testKey)
		reply, err := interceptor(ctx, str("req"), info(), handler.handle)

		must.NoError(t, err)
		test.EqOp(t, "plain string reply", reply)
	})
}

func TestInterceptor_Replay(T *testing.T) {
	T.Parallel()

	T.Run("replays the reply without running the handler", func(t *testing.T) {
		t.Parallel()

		handler := newCountingHandler(str("ch_1"))
		interceptor := newTestInterceptor(t)
		ctx := keyed(t.Context(), testKey)

		first, err := interceptor(ctx, str("req"), info(), handler.handle)
		must.NoError(t, err)

		second, err := interceptor(ctx, str("req"), info(), handler.handle)
		must.NoError(t, err)

		test.EqOp(t, int64(1), handler.Calls())

		// Rebuilt from the registry, so it is an equal message rather than the
		// same pointer.
		firstMsg, ok := first.(proto.Message)
		must.True(t, ok)
		secondMsg, ok := second.(proto.Message)
		must.True(t, ok)

		test.True(t, proto.Equal(firstMsg, secondMsg))
		test.EqOp(t, "ch_1", secondMsg.(*wrapperspb.StringValue).Value)
	})

	T.Run("replays a client-fault error", func(t *testing.T) {
		t.Parallel()

		handler := newCountingHandler(nil)
		handler.err = status.Error(codes.InvalidArgument, "bad amount")
		interceptor := newTestInterceptor(t)
		ctx := keyed(t.Context(), testKey)

		_, err := interceptor(ctx, str("req"), info(), handler.handle)
		test.EqOp(t, codes.InvalidArgument, status.Code(err))

		_, err = interceptor(ctx, str("req"), info(), handler.handle)

		// Stable answer, so it replays rather than re-running.
		test.EqOp(t, int64(1), handler.Calls())
		test.EqOp(t, codes.InvalidArgument, status.Code(err))
		test.EqOp(t, "bad amount", status.Convert(err).Message())
	})

	// A server-fault code usually means the work never landed, so pinning it
	// for the whole TTL would leave the caller unable to ever succeed.
	T.Run("does not record a server-fault error", func(t *testing.T) {
		t.Parallel()

		var mu sync.Mutex
		failing := true

		handler := &countingHandler{}
		interceptor := newTestInterceptor(t)
		ctx := keyed(t.Context(), testKey)

		handle := func(hctx context.Context, req any) (any, error) {
			mu.Lock()
			shouldFail := failing
			mu.Unlock()

			handler.calls.Add(1)
			if shouldFail {
				return nil, status.Error(codes.Unavailable, "downstream down")
			}

			return str("ch_1"), nil
		}

		_, err := interceptor(ctx, str("req"), info(), handle)
		test.EqOp(t, codes.Unavailable, status.Code(err))

		mu.Lock()
		failing = false
		mu.Unlock()

		reply, err := interceptor(ctx, str("req"), info(), handle)
		must.NoError(t, err)
		test.EqOp(t, int64(2), handler.Calls())
		test.EqOp(t, "ch_1", reply.(*wrapperspb.StringValue).Value)
	})
}

func TestInterceptor_Conflict(T *testing.T) {
	T.Parallel()

	T.Run("answers Aborted while the handler is still running", func(t *testing.T) {
		t.Parallel()

		var (
			started = make(chan struct{})
			release = make(chan struct{})
			once    sync.Once
		)

		interceptor := newTestInterceptor(t)
		ctx := keyed(t.Context(), testKey)

		handle := func(context.Context, any) (any, error) {
			once.Do(func() { close(started) })
			<-release

			return str("ch_1"), nil
		}

		go func() {
			_, _ = interceptor(ctx, str("req"), info(), handle)
		}()

		<-started

		_, err := interceptor(ctx, str("req"), info(), handle)
		close(release)

		// Aborted is gRPC's concurrency-conflict code, and its documented
		// advice — retry at a higher level — is right here.
		test.EqOp(t, codes.Aborted, status.Code(err))
	})
}

func TestInterceptor_Mismatch(T *testing.T) {
	T.Parallel()

	T.Run("a different request is InvalidArgument", func(t *testing.T) {
		t.Parallel()

		handler := newCountingHandler(str("ch_1"))
		interceptor := newTestInterceptor(t)
		ctx := keyed(t.Context(), testKey)

		_, err := interceptor(ctx, str("charge-10"), info(), handler.handle)
		must.NoError(t, err)

		_, err = interceptor(ctx, str("charge-1000"), info(), handler.handle)

		test.EqOp(t, int64(1), handler.Calls())
		test.EqOp(t, codes.InvalidArgument, status.Code(err))
	})

	T.Run("a different method is InvalidArgument", func(t *testing.T) {
		t.Parallel()

		handler := newCountingHandler(str("ch_1"))
		interceptor := newTestInterceptor(t)
		ctx := keyed(t.Context(), testKey)

		_, err := interceptor(ctx, str("req"), info(), handler.handle)
		must.NoError(t, err)

		_, err = interceptor(ctx, str("req"), infoFor("/test.Charges/Refund"), handler.handle)

		test.EqOp(t, codes.InvalidArgument, status.Code(err))
	})

	T.Run("a different principal is InvalidArgument", func(t *testing.T) {
		t.Parallel()

		type userKey struct{}

		handler := newCountingHandler(str("ch_1"))
		interceptor := newTestInterceptor(t, WithPrincipalExtractor(func(ctx context.Context) (string, error) {
			user, _ := ctx.Value(userKey{}).(string)

			return user, nil
		}))

		alice := context.WithValue(keyed(t.Context(), testKey), userKey{}, "alice")
		bob := context.WithValue(keyed(t.Context(), testKey), userKey{}, "bob")

		_, err := interceptor(alice, str("req"), info(), handler.handle)
		must.NoError(t, err)

		// Without the principal in the fingerprint bob would be handed alice's
		// reply.
		_, err = interceptor(bob, str("req"), info(), handler.handle)
		test.EqOp(t, codes.InvalidArgument, status.Code(err))
	})

	// Map fields serialize in a random order unless marshaling is
	// deterministic, so without that an ordinary retry would look like reuse.
	T.Run("a message with map fields is stable across attempts", func(t *testing.T) {
		t.Parallel()

		build := func() *structpb.Struct {
			s, err := structpb.NewStruct(map[string]any{
				"a": "1", "b": "2", "c": "3", "d": "4", "e": "5",
				"f": "6", "g": "7", "h": "8", "i": "9", "j": "10",
			})
			must.NoError(t, err)

			return s
		}

		handler := newCountingHandler(str("ch_1"))
		interceptor := newTestInterceptor(t)
		ctx := keyed(t.Context(), testKey)

		_, err := interceptor(ctx, build(), info(), handler.handle)
		must.NoError(t, err)

		for range 20 {
			_, err = interceptor(ctx, build(), info(), handler.handle)
			must.NoError(t, err)
		}

		test.EqOp(t, int64(1), handler.Calls())
	})
}

func TestInterceptor_Truncation(T *testing.T) {
	T.Parallel()

	// The call is known to have succeeded, so re-running it is not an option.
	// Reporting the reply as gone preserves the guarantee and is honest.
	T.Run("records an over-sized reply and refuses to replay it", func(t *testing.T) {
		t.Parallel()

		handler := newCountingHandler(str(strings.Repeat("a", 512)))
		interceptor := newTestInterceptor(t, WithMaxResponseBytes(16))
		ctx := keyed(t.Context(), testKey)

		first, err := interceptor(ctx, str("req"), info(), handler.handle)
		must.NoError(t, err)
		test.EqOp(t, 512, len(first.(*wrapperspb.StringValue).Value))

		_, err = interceptor(ctx, str("req"), info(), handler.handle)

		test.EqOp(t, int64(1), handler.Calls())
		test.EqOp(t, codes.ResourceExhausted, status.Code(err))
	})
}

func TestInterceptor_KeyValidation(T *testing.T) {
	T.Parallel()

	T.Run("an invalid key is InvalidArgument", func(t *testing.T) {
		t.Parallel()

		handler := newCountingHandler(str("ch_1"))
		interceptor := newTestInterceptor(t)

		for _, key := range []string{strings.Repeat("k", 300), "has space"} {
			_, err := interceptor(keyed(t.Context(), key), str("req"), info(), handler.handle)
			test.EqOp(t, codes.InvalidArgument, status.Code(err))
		}

		test.EqOp(t, int64(0), handler.Calls())
	})
}

func TestInterceptor_StoreFailure(T *testing.T) {
	T.Parallel()

	T.Run("fails closed without running the handler", func(t *testing.T) {
		t.Parallel()

		handler := newCountingHandler(str("ch_1"))
		interceptor := newInterceptorFor(t, newFailingStoreManager(t))

		_, err := interceptor(keyed(t.Context(), testKey), str("req"), info(), handler.handle)

		test.EqOp(t, codes.Internal, status.Code(err))
		test.EqOp(t, int64(0), handler.Calls())
	})

	T.Run("fails open by running the handler when configured to", func(t *testing.T) {
		t.Parallel()

		handler := newCountingHandler(str("ch_1"))
		interceptor := newInterceptorFor(t, newFailingStoreManager(t,
			idempotency.WithStoreFailurePolicy[Response](idempotency.FailOpen),
		))

		reply, err := interceptor(keyed(t.Context(), testKey), str("req"), info(), handler.handle)

		must.NoError(t, err)
		test.EqOp(t, int64(1), handler.Calls())
		test.EqOp(t, "ch_1", reply.(*wrapperspb.StringValue).Value)
	})
}

func TestRecordable(T *testing.T) {
	T.Parallel()

	recorded := []codes.Code{
		codes.OK, codes.InvalidArgument, codes.NotFound, codes.AlreadyExists,
		codes.PermissionDenied, codes.Unauthenticated, codes.FailedPrecondition, codes.OutOfRange,
	}
	refused := []codes.Code{
		codes.Internal, codes.Unavailable, codes.Unknown, codes.DeadlineExceeded,
		codes.ResourceExhausted, codes.Aborted, codes.DataLoss, codes.Canceled, codes.Unimplemented,
	}

	T.Run("records client-fault outcomes", func(t *testing.T) {
		t.Parallel()

		for _, code := range recorded {
			test.True(t, Recordable(&Response{StatusCode: uint32(code)}))
		}
	})

	T.Run("refuses server-fault outcomes", func(t *testing.T) {
		t.Parallel()

		for _, code := range refused {
			test.False(t, Recordable(&Response{StatusCode: uint32(code)}))
		}
	})
}
