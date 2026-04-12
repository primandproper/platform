package oteltrace

import (
	"errors"
	"testing"

	"github.com/primandproper/platform/observability/logging"

	"github.com/shoenig/test"
)

func Test_tracingErrorHandler_Handle(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		errorHandler{logger: logging.NewNoopLogger()}.Handle(errors.New("blah"))
	})
}

func TestConfig_SetupOtelHTTP(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		cfg := &Config{
			CollectorEndpoint: "blah blah blah",
		}

		actual, err := SetupOtelGRPC(ctx, t.Name(), 0, cfg)
		test.NoError(t, err)
		test.NotNil(t, actual)
	})
}
