package tableaccess

import (
	"testing"

	"github.com/primandproper/platform-go/v13/observability"
	"github.com/primandproper/platform-go/v13/observability/logging"
	loggingnoop "github.com/primandproper/platform-go/v13/observability/logging/noop"
	metricsnoop "github.com/primandproper/platform-go/v13/observability/metrics/noop"
	tracingnoop "github.com/primandproper/platform-go/v13/observability/tracing/noop"

	"github.com/shoenig/test"
)

func TestOptions(T *testing.T) {
	T.Parallel()

	T.Run("the zero configuration works", func(t *testing.T) {
		t.Parallel()

		// An absent logger logs nowhere and an absent tracer provider traces
		// nowhere, so a caller who wants neither names neither.
		o := newOptions(nil)

		test.Nil(t, o.logger)
		test.Nil(t, o.tracerProvider)
	})

	T.Run("each option sets the field it names", func(t *testing.T) {
		t.Parallel()

		var logger logging.Logger = loggingnoop.NewLogger()
		tracerProvider := tracingnoop.NewTracerProvider()

		o := newOptions([]Option{
			WithLogger(logger),
			WithTracerProvider(tracerProvider),
		})

		test.Eq(t, logger, o.logger)
		test.Eq(t, tracerProvider, o.tracerProvider)
	})

	T.Run("nil options are ignored", func(t *testing.T) {
		t.Parallel()

		o := newOptions([]Option{nil})

		test.Nil(t, o.logger)
		test.Nil(t, o.tracerProvider)
	})

	T.Run("WithPillars supplies both dependencies this package takes", func(t *testing.T) {
		t.Parallel()

		pillars := &observability.Pillars{
			Logger:          loggingnoop.NewLogger(),
			TracerProvider:  tracingnoop.NewTracerProvider(),
			MetricsProvider: metricsnoop.NewMetricsProvider(),
		}

		o := newOptions([]Option{WithPillars(pillars)})

		test.Eq(t, pillars.Logger, o.logger)
		test.Eq(t, pillars.TracerProvider, o.tracerProvider)
		// The metrics provider is ignored: everything here is an administrative
		// act performed a handful of times over a deployment's life, and a
		// counter over those is a number nobody has a question for.
	})

	T.Run("a later option overrides what the pillars supplied", func(t *testing.T) {
		t.Parallel()

		o := newOptions([]Option{
			WithPillars(&observability.Pillars{
				Logger:         loggingnoop.NewLogger(),
				TracerProvider: tracingnoop.NewTracerProvider(),
			}),
			WithTracerProvider(nil),
		})

		test.NotNil(t, o.logger)
		test.Nil(t, o.tracerProvider)
	})
}
