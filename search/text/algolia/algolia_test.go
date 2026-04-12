package algolia

import (
	"testing"

	cbnoop "github.com/primandproper/platform/circuitbreaking/noop"
	"github.com/primandproper/platform/observability/logging"
	"github.com/primandproper/platform/observability/tracing"

	"github.com/shoenig/test"
)

type example struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func TestProvideIndexManager(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		logger := logging.NewNoopLogger()
		tracerProvider := tracing.NewNoopTracerProvider()

		im, err := ProvideIndexManager[example](logger, tracerProvider, &Config{}, "test", cbnoop.NewCircuitBreaker())
		test.NoError(t, err)
		test.NotNil(t, im)
	})

	T.Run("with nil config", func(t *testing.T) {
		t.Parallel()

		logger := logging.NewNoopLogger()
		tracerProvider := tracing.NewNoopTracerProvider()

		im, err := ProvideIndexManager[example](logger, tracerProvider, nil, "test", cbnoop.NewCircuitBreaker())
		test.Error(t, err)
		test.Nil(t, im)
	})
}
