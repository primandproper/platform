package msgconfig

import (
	"testing"

	"github.com/primandproper/platform/messagequeue"
	"github.com/primandproper/platform/observability/logging"
	"github.com/primandproper/platform/observability/metrics"
	"github.com/primandproper/platform/observability/tracing"

	"github.com/samber/do/v2"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestRegisterMessageQueue(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		i := do.New()
		do.ProvideValue(i, t.Context())
		do.ProvideValue(i, logging.NewNoopLogger())
		do.ProvideValue(i, tracing.NewNoopTracerProvider())
		do.ProvideValue[metrics.Provider](i, nil)
		do.ProvideValue(i, &Config{})

		RegisterMessageQueue(i)

		consumer, err := do.Invoke[messagequeue.ConsumerProvider](i)
		must.NoError(t, err)
		test.NotNil(t, consumer)

		publisher, err := do.Invoke[messagequeue.PublisherProvider](i)
		must.NoError(t, err)
		test.NotNil(t, publisher)
	})
}
