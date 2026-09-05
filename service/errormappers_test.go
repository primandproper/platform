package service

import (
	"context"
	"testing"

	"github.com/primandproper/platform-go/v14/dataprivacy"
	dataprivacycfg "github.com/primandproper/platform-go/v14/dataprivacy/config"
	platformerrors "github.com/primandproper/platform-go/v14/errors"
	grpcerrors "github.com/primandproper/platform-go/v14/errors/grpc"
	httperrors "github.com/primandproper/platform-go/v14/errors/http"
	"github.com/primandproper/platform-go/v14/links"
	"github.com/primandproper/platform-go/v14/operations"
	operationscfg "github.com/primandproper/platform-go/v14/operations/config"
	"github.com/primandproper/platform-go/v14/sessions"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestRegister_installsTheDomainErrorMappers is the other end of the inversion
// that moved these mappings out of errors/http and errors/grpc. Those two are
// primitives and answer only for primitives now, so a privacy request nobody
// registered a mapper for reaches a subject as a 500 — the exact failure the
// mappings were written to end. This is what registers them.
//
// It asserts through ToAPIError and MapToGRPC rather than through the mappers
// themselves, because the registration is the whole of what this file is about:
// the mappers' own cases are tested in their own packages.
func TestRegister_installsTheDomainErrorMappers(T *testing.T) {
	T.Parallel()

	cfg := &Config{
		Name:        "example",
		DataPrivacy: &dataprivacycfg.Config{},
		Operations:  &operationscfg.Config{},
	}
	newInjector(T, cfg)

	// Wrapped, because that is how one arrives from a handler.
	for name, sentinel := range map[string]error{
		"dataprivacy": dataprivacy.ErrRequestNotFound,
		"links":       links.ErrLinkExpired,
		"operations":  operations.ErrOperationNotFound,
		"sessions":    sessions.ErrExpired,
	} {
		T.Run(name, func(t *testing.T) {
			t.Parallel()

			err := platformerrors.Wrap(sentinel, "serving a request")

			code, msg := httperrors.ToAPIError(err)
			test.NotEqOp(t, httperrors.ErrNothingSpecific, code, test.Sprintf(
				"%v resolved to the neutral code, so no HTTP mapper was registered for %s", sentinel, name))
			test.NotEqOp(t, "an error occurred", msg)

			test.NotEqOp(t, codes.Unknown, grpcerrors.MapToGRPC(err, codes.Unknown), test.Sprintf(
				"%v resolved to codes.Unknown, so no gRPC mapper was registered for %s", sentinel, name))
		})
	}
}

// TestRegister_installsTheClientSafeSentinels covers the other half of what
// links needs from gRPC. Its four redemption outcomes share one code, so the
// message is the only place the difference between "already used" and "expired"
// survives, and a gRPC message is the code's name unless a sentinel is
// registered as safe to quote.
func TestRegister_installsTheClientSafeSentinels(T *testing.T) {
	T.Parallel()

	newInjector(T, &Config{Name: "example"})

	interceptor := grpcerrors.UnaryErrorEncodingInterceptor()

	seen := map[string]struct{}{}

	for _, sentinel := range links.ClientSafeSentinels {
		_, err := interceptor(
			T.Context(),
			nil,
			&grpc.UnaryServerInfo{},
			func(context.Context, any) (any, error) {
				return nil, platformerrors.Wrap(sentinel, "redeeming action link")
			},
		)
		must.Error(T, err)

		st, ok := status.FromError(err)
		must.True(T, ok)

		test.EqOp(T, sentinel.Error(), st.Message(), test.Sprintf(
			"%v reached the client as %q, which is the code's name rather than its own", sentinel, st.Message()))

		seen[st.Message()] = struct{}{}
	}

	test.MapLen(T, len(links.ClientSafeSentinels), seen)
}
