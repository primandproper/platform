package multisource

import (
	"testing"

	analyticscfg "github.com/primandproper/platform/analytics/config"
	"github.com/primandproper/platform/analytics/posthog"
	"github.com/primandproper/platform/analytics/segment"
	"github.com/primandproper/platform/observability/logging"
	"github.com/primandproper/platform/observability/metrics"
	"github.com/primandproper/platform/observability/tracing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestProvideMultiSourceEventReporter(T *testing.T) {
	T.Parallel()

	T.Run("with no proxy sources", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()

		reporter, err := ProvideMultiSourceEventReporter(ctx, nil, logging.NewNoopLogger(), tracing.NewNoopTracerProvider(), metrics.NewNoopMetricsProvider())
		must.NoError(t, err)
		must.NotNil(t, reporter)
		test.MapEmpty(t, reporter.reporters)
	})

	T.Run("with valid segment source", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		sources := map[string]*analyticscfg.SourceConfig{
			"ios": {
				Provider: analyticscfg.ProviderSegment,
				Segment:  &segment.Config{APIToken: t.Name()},
			},
		}

		reporter, err := ProvideMultiSourceEventReporter(ctx, sources, logging.NewNoopLogger(), tracing.NewNoopTracerProvider(), metrics.NewNoopMetricsProvider())
		must.NoError(t, err)
		must.NotNil(t, reporter)
		test.MapLen(t, 1, reporter.reporters)
	})

	T.Run("with invalid source falls back to noop", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		sources := map[string]*analyticscfg.SourceConfig{
			"ios": {
				Provider: analyticscfg.ProviderSegment,
				Segment:  &segment.Config{},
			},
		}

		reporter, err := ProvideMultiSourceEventReporter(ctx, sources, logging.NewNoopLogger(), tracing.NewNoopTracerProvider(), metrics.NewNoopMetricsProvider())
		must.NoError(t, err)
		must.NotNil(t, reporter)
		test.MapLen(t, 1, reporter.reporters)
	})

	T.Run("with unrecognized provider uses noop", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		sources := map[string]*analyticscfg.SourceConfig{
			"web": {
				Provider: "bogus",
			},
		}

		reporter, err := ProvideMultiSourceEventReporter(ctx, sources, logging.NewNoopLogger(), tracing.NewNoopTracerProvider(), metrics.NewNoopMetricsProvider())
		must.NoError(t, err)
		must.NotNil(t, reporter)
		test.MapLen(t, 1, reporter.reporters)
	})

	T.Run("with multiple posthog sources reuses shared reporter", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		sources := map[string]*analyticscfg.SourceConfig{
			"ios": {
				Provider: analyticscfg.ProviderPostHog,
				Posthog:  &posthog.Config{APIKey: t.Name()},
			},
			"web": {
				Provider: analyticscfg.ProviderPostHog,
				Posthog:  &posthog.Config{APIKey: t.Name()},
			},
		}

		reporter, err := ProvideMultiSourceEventReporter(ctx, sources, logging.NewNoopLogger(), tracing.NewNoopTracerProvider(), metrics.NewNoopMetricsProvider())
		must.NoError(t, err)
		must.NotNil(t, reporter)
		test.MapLen(t, 2, reporter.reporters)
	})

	T.Run("with empty proxy sources map", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		sources := map[string]*analyticscfg.SourceConfig{}

		reporter, err := ProvideMultiSourceEventReporter(ctx, sources, logging.NewNoopLogger(), tracing.NewNoopTracerProvider(), metrics.NewNoopMetricsProvider())
		must.NoError(t, err)
		must.NotNil(t, reporter)
		test.MapEmpty(t, reporter.reporters)
	})
}
