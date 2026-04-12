package http

import (
	"testing"
	"time"

	"github.com/primandproper/platform/observability/logging"
	"github.com/primandproper/platform/observability/tracing"
	"github.com/primandproper/platform/routing"

	"github.com/samber/do/v2"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestRegisterHTTPServer(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		i := do.New()
		do.ProvideValue(i, Config{Port: 8080, StartupDeadline: time.Second})
		do.ProvideValue(i, logging.NewNoopLogger())
		do.ProvideValue[routing.Router](i, nil)
		do.ProvideValue(i, tracing.NewNoopTracerProvider())

		RegisterHTTPServer(i, "test_service")

		srv, err := do.Invoke[Server](i)
		must.NoError(t, err)
		test.NotNil(t, srv)
	})
}
