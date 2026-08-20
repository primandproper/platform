package grpc

import (
	"context"
	"net"
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func TestResolveMessageSize(T *testing.T) {
	T.Parallel()

	T.Run("nothing configured takes the default", func(t *testing.T) {
		t.Parallel()

		size, err := resolveMessageSize("send", 0, 0)

		must.NoError(t, err)
		test.EqOp(t, DefaultMaxMessageSize, size)
	})

	T.Run("the config field is used when no option names one", func(t *testing.T) {
		t.Parallel()

		size, err := resolveMessageSize("send", 1<<20, 0)

		must.NoError(t, err)
		test.EqOp(t, 1<<20, size)
	})

	T.Run("the option wins over the config field", func(t *testing.T) {
		t.Parallel()

		// Caller options are applied last everywhere else in this module, and
		// this one is no exception.
		size, err := resolveMessageSize("send", 1<<20, 2<<20)

		must.NoError(t, err)
		test.EqOp(t, 2<<20, size)
	})

	T.Run("the largest expressible bound is accepted", func(t *testing.T) {
		t.Parallel()

		size, err := resolveMessageSize("receive", 0, UnboundedMessageSize)

		must.NoError(t, err)
		test.EqOp(t, UnboundedMessageSize, size)
	})

	T.Run("a negative bound is refused", func(t *testing.T) {
		t.Parallel()

		size, err := resolveMessageSize("receive", 0, -1)

		must.Error(t, err)
		test.EqOp(t, 0, size)
		test.StrContains(t, err.Error(), "receive")
	})

	T.Run("a bound past the wire's ceiling is refused rather than clamped", func(t *testing.T) {
		t.Parallel()

		// Clamping would leave the server running under a bound nobody asked
		// for, which is the failure this setting exists to make visible.
		size, err := resolveMessageSize("send", UnboundedMessageSize+1, 0)

		must.Error(t, err)
		test.EqOp(t, 0, size)
		test.StrContains(t, err.Error(), "send")
	})
}

func TestNewGRPCServer_messageSizeBounds(T *testing.T) {
	T.Parallel()

	T.Run("refuses a negative config bound", func(t *testing.T) {
		t.Parallel()

		srv, err := NewGRPCServer(t.Context(), &Config{MaxReceiveMessageSize: -1}, nil, nil, nil)

		test.Nil(t, srv)
		test.Error(t, err)
	})

	T.Run("refuses a negative option bound the config never sees", func(t *testing.T) {
		t.Parallel()

		// The config validated clean; the option is the only place this number
		// came from, so the constructor has to be the one that catches it.
		srv, err := NewGRPCServer(t.Context(), &Config{}, nil, nil, nil, WithMaxSendMessageSize(-1))

		test.Nil(t, srv)
		test.Error(t, err)
	})

	T.Run("refuses an option bound past the wire's ceiling", func(t *testing.T) {
		t.Parallel()

		srv, err := NewGRPCServer(t.Context(), &Config{}, nil, nil, nil, WithMaxReceiveMessageSize(UnboundedMessageSize+1))

		test.Nil(t, srv)
		test.Error(t, err)
	})
}

// The end-to-end half: the bounds above are only worth anything if they reach
// grpc.NewServer, which nothing short of a real RPC can show.

const oversized = DefaultMaxMessageSize + (1 << 20)

func TestServer_sendMessageSize(T *testing.T) {
	T.Parallel()

	T.Run("the default bounds send, so an oversized response fails on the server", func(t *testing.T) {
		t.Parallel()

		// The client's own receive bound is lifted, so the only party left that
		// can refuse this response is the server that produced it — which is the
		// whole point of bounding send at all.
		err := echoRoundTrip(t, oversized, nil, grpc.MaxCallRecvMsgSize(UnboundedMessageSize))

		must.Error(t, err)
		test.EqOp(t, codes.ResourceExhausted, status.Code(err))
		test.StrContains(t, err.Error(), "trying to send message larger than max")
	})

	T.Run("a raised config bound lets the same response through", func(t *testing.T) {
		t.Parallel()

		err := echoRoundTrip(t, oversized, &Config{MaxSendMessageSize: oversized + 16}, grpc.MaxCallRecvMsgSize(UnboundedMessageSize))

		test.NoError(t, err)
	})

	T.Run("a raised option bound lets the same response through", func(t *testing.T) {
		t.Parallel()

		err := echoRoundTrip(t, oversized, nil, grpc.MaxCallRecvMsgSize(UnboundedMessageSize), withServerOption(WithMaxSendMessageSize(oversized+16)))

		test.NoError(t, err)
	})

	T.Run("the option wins over the config field", func(t *testing.T) {
		t.Parallel()

		// Config says the response fits and the option says it does not; the
		// option is applied last, so it decides.
		err := echoRoundTrip(t, oversized, &Config{MaxSendMessageSize: oversized + 16},
			grpc.MaxCallRecvMsgSize(UnboundedMessageSize), withServerOption(WithMaxSendMessageSize(1<<20)))

		must.Error(t, err)
		test.EqOp(t, codes.ResourceExhausted, status.Code(err))
	})
}

func TestServer_receiveMessageSize(T *testing.T) {
	T.Parallel()

	T.Run("the default bounds receive", func(t *testing.T) {
		t.Parallel()

		err := echoRoundTrip(t, 0, nil, grpc.MaxCallSendMsgSize(UnboundedMessageSize), withRequestBytes(oversized))

		must.Error(t, err)
		test.EqOp(t, codes.ResourceExhausted, status.Code(err))
	})

	T.Run("a raised config bound lets the same request through", func(t *testing.T) {
		t.Parallel()

		err := echoRoundTrip(t, 0, &Config{MaxReceiveMessageSize: oversized + 16},
			grpc.MaxCallSendMsgSize(UnboundedMessageSize), withRequestBytes(oversized))

		test.NoError(t, err)
	})

	T.Run("a raised option bound lets the same request through", func(t *testing.T) {
		t.Parallel()

		err := echoRoundTrip(t, 0, nil, grpc.MaxCallSendMsgSize(UnboundedMessageSize),
			withRequestBytes(oversized), withServerOption(WithMaxReceiveMessageSize(oversized+16)))

		test.NoError(t, err)
	})
}

// echoOptions collects what an echo round trip needs beyond its response size.
type echoOptions struct {
	serverOptions []Option
	requestBytes  int
}

type echoOption func(*echoOptions)

func withServerOption(opt Option) echoOption {
	return func(e *echoOptions) { e.serverOptions = append(e.serverOptions, opt) }
}

func withRequestBytes(n int) echoOption {
	return func(e *echoOptions) { e.requestBytes = n }
}

// echoRoundTrip stands a server up over the echo service, calls it once, and
// reports what the call answered. responseBytes is how much the handler sends
// back; callOptions are the client's own bounds, lifted so that the server is
// the only party that can refuse anything.
func echoRoundTrip(t *testing.T, responseBytes int, cfg *Config, args ...any) error {
	t.Helper()

	e := &echoOptions{}
	var callOptions []grpc.CallOption
	for _, arg := range args {
		switch a := arg.(type) {
		case grpc.CallOption:
			callOptions = append(callOptions, a)
		case echoOption:
			a(e)
		default:
			t.Fatalf("unexpected argument %T", arg)
		}
	}

	if cfg == nil {
		cfg = &Config{}
	}

	srv, err := NewGRPCServer(t.Context(), cfg, nil, nil,
		[]RegistrationFunc{func(s *grpc.Server) {
			s.RegisterService(&echoServiceDesc, &echoService{responseBytes: responseBytes})
		}},
		e.serverOptions...)
	must.NoError(t, err)

	cc := dialEcho(t, serveEcho(t, srv), callOptions...)

	return cc.Invoke(t.Context(), echoMethod, wrapperspb.Bytes(make([]byte, e.requestBytes)), new(wrapperspb.BytesValue))
}

// serveEcho serves the server on an ephemeral loopback port and returns its
// address. It drives srv.grpcServer rather than Serve because Serve binds the
// configured port, and a test that wants an ephemeral one has no way to learn
// which port it got.
func serveEcho(t *testing.T, srv *Server) string {
	t.Helper()

	lis, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	must.NoError(t, err)

	served := make(chan struct{})
	go func() {
		defer close(served)

		_ = srv.grpcServer.Serve(lis)
	}()

	t.Cleanup(func() {
		srv.grpcServer.Stop()
		<-served
	})

	return lis.Addr().String()
}

func dialEcho(t *testing.T, addr string, callOptions ...grpc.CallOption) *grpc.ClientConn {
	t.Helper()

	cc, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(callOptions...),
	)
	must.NoError(t, err)
	t.Cleanup(func() { _ = cc.Close() })

	return cc
}

const echoMethod = "/platform.test.v1.Echo/Echo"

// echoService answers with a payload of a size the test chose, which is the
// only thing a message-size bound can be shown against.
type echoService struct {
	responseBytes int
}

var echoServiceDesc = grpc.ServiceDesc{
	ServiceName: "platform.test.v1.Echo",
	HandlerType: (*any)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "Echo",
			Handler: func(srv any, _ context.Context, dec func(any) error, _ grpc.UnaryServerInterceptor) (any, error) {
				in := new(wrapperspb.BytesValue)
				if err := dec(in); err != nil {
					return nil, err
				}

				return wrapperspb.Bytes(make([]byte, srv.(*echoService).responseBytes)), nil
			},
		},
	},
}
