package grpc

import (
	"github.com/primandproper/platform-go/v9/observability"

	"github.com/samber/do/v2"
	"google.golang.org/grpc"
)

// RegisterGRPCServer registers a *Server with the injector.
// Prerequisites: []grpc.UnaryServerInterceptor, []grpc.StreamServerInterceptor,
// and []RegistrationFunc must be registered in the injector before calling this.
func RegisterGRPCServer(i do.Injector) {
	do.Provide[*Server](i, func(i do.Injector) (*Server, error) {
		pillars, err := observability.InvokePillars(i)
		if err != nil {
			return nil, err
		}

		return NewGRPCServer(
			do.MustInvoke[*Config](i),
			do.MustInvoke[[]grpc.UnaryServerInterceptor](i),
			do.MustInvoke[[]grpc.StreamServerInterceptor](i),
			do.MustInvoke[[]RegistrationFunc](i),
			WithLogger(pillars.Logger),
			WithTracerProvider(pillars.TracerProvider),
		)
	})
}
