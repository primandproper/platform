package identitycfg

import (
	"context"

	"github.com/primandproper/platform-go/v14/database"
	"github.com/primandproper/platform-go/v14/identity"
	identitygrpc "github.com/primandproper/platform-go/v14/identity/grpc"
	"github.com/primandproper/platform-go/v14/observability"

	"github.com/samber/do/v2"
)

// RegisterStore registers an identity.Store with the injector.
//
// Prerequisites: *Config and database.Client must be registered in the injector
// before the Store is invoked.
func RegisterStore(i do.Injector) {
	do.Provide(i, func(i do.Injector) (identity.Store, error) {
		pillars, err := observability.InvokePillars(i)
		if err != nil {
			return nil, err
		}

		return NewStore(
			do.MustInvoke[context.Context](i),
			do.MustInvoke[*Config](i),
			do.MustInvoke[database.Client](i),
			WithPillars(pillars),
		)
	})
}

// RegisterService registers an *identity.Service with the injector.
//
// Prerequisites: *Config, database.Client and identity.Store (see RegisterStore)
// must be registered before the Service is invoked.
//
// identity.Hooks is resolved if something registered one and defaulted to
// identity.NoopHooks otherwise, which is the same reading the constructor takes:
// an application with nothing to commit beside an identity write registers
// nothing. It is do.InvokeAs rather than MustInvoke for exactly that reason — a
// missing Hooks is a configuration, not a failure.
func RegisterService(i do.Injector) {
	do.Provide(i, func(i do.Injector) (*identity.Service, error) {
		pillars, err := observability.InvokePillars(i)
		if err != nil {
			return nil, err
		}

		opts := []Option{WithPillars(pillars)}

		if hooks, hooksErr := do.Invoke[identity.Hooks](i); hooksErr == nil {
			opts = append(opts, WithHooks(hooks))
		}

		return NewService(
			do.MustInvoke[context.Context](i),
			do.MustInvoke[*Config](i),
			do.MustInvoke[database.Client](i),
			do.MustInvoke[identity.Store](i),
			opts...,
		)
	})
}

// RegisterServer registers an *identitygrpc.Server with the injector.
//
// Prerequisites: *Config, database.Client, identity.Store (see RegisterStore),
// *identity.Service (see RegisterService) and an
// identitygrpc.PrincipalExtractor must be registered before the Server is
// invoked.
//
// The extractor is a MustInvoke where Hooks above is not, and the asymmetry is
// the point: a server with no hooks writes nothing extra, and a server with no
// way to resolve a caller answers every read with the zero scope. The first is a
// configuration and the second is a hole, so the container refuses to build one.
// Register it with do.ProvideValue, keyed on the named type:
//
//	do.ProvideValue[identitygrpc.PrincipalExtractor](i, principalFromContext)
//
// Registering the server does not declare its authorization requirements or
// install its error mappings. See NewServer for what a mount still owes.
func RegisterServer(i do.Injector) {
	do.Provide(i, func(i do.Injector) (*identitygrpc.Server, error) {
		pillars, err := observability.InvokePillars(i)
		if err != nil {
			return nil, err
		}

		return NewServer(
			do.MustInvoke[context.Context](i),
			do.MustInvoke[*Config](i),
			do.MustInvoke[database.Client](i),
			do.MustInvoke[*identity.Service](i),
			do.MustInvoke[identity.Store](i),
			do.MustInvoke[identitygrpc.PrincipalExtractor](i),
			WithPillars(pillars),
		)
	})
}
