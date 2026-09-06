package oauth2servercfg

import (
	"testing"

	"github.com/primandproper/platform-go/v14/authentication/oauth2server"
	oauth2memory "github.com/primandproper/platform-go/v14/authentication/oauth2server/memory"
	"github.com/primandproper/platform-go/v14/observability"
	loggingnoop "github.com/primandproper/platform-go/v14/observability/logging/noop"
	metricsnoop "github.com/primandproper/platform-go/v14/observability/metrics/noop"
	tracingnoop "github.com/primandproper/platform-go/v14/observability/tracing/noop"

	"github.com/shoenig/test"
)

func TestOptions(T *testing.T) {
	T.Parallel()

	T.Run("absent means nothing rather than a noop nobody asked for", func(t *testing.T) {
		t.Parallel()

		o := newOptions(nil)

		// Every constructor below this resolves an absent pillar through
		// EnsureLogger and friends, so nothing here has to supply one.
		test.Nil(t, o.logger)
		test.Nil(t, o.tracerProvider)
		test.Nil(t, o.metricsProvider)
		test.SliceEmpty(t, o.server)
		test.SliceEmpty(t, o.memoryStore)
	})

	T.Run("a nil option is ignored rather than dereferenced", func(t *testing.T) {
		t.Parallel()

		test.NotNil(t, newOptions([]Option{nil, WithLogger(loggingnoop.NewLogger())}))
	})

	T.Run("the three pillars are set one at a time", func(t *testing.T) {
		t.Parallel()

		o := newOptions([]Option{
			WithLogger(loggingnoop.NewLogger()),
			WithTracerProvider(tracingnoop.NewTracerProvider()),
			WithMetricsProvider(metricsnoop.NewMetricsProvider()),
		})

		test.NotNil(t, o.logger)
		test.NotNil(t, o.tracerProvider)
		test.NotNil(t, o.metricsProvider)
	})

	T.Run("WithPillars sets all three at once", func(t *testing.T) {
		t.Parallel()

		o := newOptions([]Option{WithPillars(&observability.Pillars{
			Logger:          loggingnoop.NewLogger(),
			TracerProvider:  tracingnoop.NewTracerProvider(),
			MetricsProvider: metricsnoop.NewMetricsProvider(),
		})})

		test.NotNil(t, o.logger)
		test.NotNil(t, o.tracerProvider)
		test.NotNil(t, o.metricsProvider)
	})

	T.Run("a later option overrides what WithPillars supplied", func(t *testing.T) {
		t.Parallel()

		// Options apply in order, which is what lets a caller hand over its
		// pillars and then leave one component unmetered.
		o := newOptions([]Option{
			WithPillars(&observability.Pillars{
				Logger:          loggingnoop.NewLogger(),
				MetricsProvider: metricsnoop.NewMetricsProvider(),
			}),
			WithMetricsProvider(nil),
		})

		test.NotNil(t, o.logger)
		test.Nil(t, o.metricsProvider)
	})

	T.Run("the pass-through slices accumulate", func(t *testing.T) {
		t.Parallel()

		// Go allows one variadic per function and that slot belongs to this
		// package's own Option, so anything bound for a component this builds
		// arrives through one of these.
		o := newOptions([]Option{
			WithServerOptions(oauth2server.WithScopes("read")),
			WithServerOptions(oauth2server.WithScopes("write")),
			WithMemoryStoreOptions(oauth2memory.WithLogger(loggingnoop.NewLogger())),
		})

		test.SliceLen(t, 2, o.server)
		test.SliceLen(t, 1, o.memoryStore)
	})
}
