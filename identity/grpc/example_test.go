package grpc_test

import (
	"context"
	"fmt"

	"github.com/primandproper/platform-go/v14/authorization"
	authzgrpc "github.com/primandproper/platform-go/v14/authorization/grpc"
	"github.com/primandproper/platform-go/v14/database"
	"github.com/primandproper/platform-go/v14/errormappers"
	grpcerrors "github.com/primandproper/platform-go/v14/errors/grpc"
	"github.com/primandproper/platform-go/v14/identity"
	identitygrpc "github.com/primandproper/platform-go/v14/identity/grpc"
	"github.com/primandproper/platform-go/v14/observability"
	grpcserver "github.com/primandproper/platform-go/v14/server/grpc"

	"google.golang.org/grpc"
)

// Example_mount is the acceptance test for this package's seams, written as the
// thing it claims: turning the directory on is a config block, a Hooks value, an
// optional permission override, and one registration.
//
// Everything a consumer supplies here is something only they can: the database,
// how somebody proved who they are, what else to commit alongside an identity
// write, and which of their roles may do what. Nothing in it is boilerplate this
// package could have written and did not.
func Example_mount() {
	var (
		ctx       context.Context
		client    database.Client
		pillars   *observability.Pillars
		cfg       *grpcserver.Config
		authn     grpc.UnaryServerInterceptor     // yours: puts a Principal on the context
		principal identitygrpc.PrincipalExtractor // yours: reads it back off
		grants    authorization.GrantsExtractor   // yours: the caller's authority
		hooks     identity.Hooks                  // yours: what commits alongside a write
	)

	_ = func() error {
		// 1. The store and the operations over it.
		store, err := identity.NewSQLStore(client, identity.WithStorePillars(pillars))
		if err != nil {
			return err
		}

		svc, err := identity.NewService(client, store,
			identity.WithHooks(hooks), identity.WithServicePillars(pillars))
		if err != nil {
			return err
		}

		// 2. The transport.
		srv, err := identitygrpc.NewServer(client, svc, store, principal,
			identitygrpc.WithPillars(pillars))
		if err != nil {
			return err
		}

		// 3. The permissions: this package's defaults, plus whatever the
		//    consumer overrides. The self-service methods are declared Public
		//    to the enforcer and checked against the caller inside each RPC.
		builder := authzgrpc.NewRequirements().RequireAll(identitygrpc.Permissions())
		for _, method := range identitygrpc.SelfServiceMethods() {
			builder.Public(method)
		}

		reqs, err := builder.Build()
		if err != nil {
			return err
		}

		enforcer, err := authzgrpc.NewEnforcer(reqs, grants)
		if err != nil {
			return err
		}

		// 4. The error mappings. One call, and not optional: without it a taken
		//    username reaches a client as codes.Unknown.
		errormappers.Register()

		// 5. The mount.
		_, err = grpcserver.NewGRPCServer(ctx, cfg,
			[]grpc.UnaryServerInterceptor{
				grpcerrors.UnaryErrorEncodingInterceptor(),
				authn,
				enforcer.UnaryServerInterceptor(),
			},
			nil,
			[]grpcserver.RegistrationFunc{srv.RegisterOn},
		)

		return err
	}

	fmt.Println("mounted")
	// Output: mounted
}
