package grpc

import (
	"testing"

	"github.com/primandproper/platform/observability/logging"
	"github.com/primandproper/platform/observability/tracing"

	"github.com/samber/do/v2"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
	"google.golang.org/grpc"
)

func TestRegisterGRPCServer(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		i := do.New()
		do.ProvideValue(i, &Config{Port: 0})
		do.ProvideValue(i, logging.NewNoopLogger())
		do.ProvideValue(i, tracing.NewNoopTracerProvider())
		do.ProvideValue(i, []grpc.UnaryServerInterceptor(nil))
		do.ProvideValue(i, []grpc.StreamServerInterceptor(nil))
		do.ProvideValue(i, []RegistrationFunc(nil))

		RegisterGRPCServer(i)

		srv, err := do.Invoke[*Server](i)
		must.NoError(t, err)
		test.NotNil(t, srv)
	})
}
