package multisource

import (
	"context"

	analyticscfg "github.com/primandproper/platform-go/v9/analytics/config"
	"github.com/primandproper/platform-go/v9/observability/logging"
	"github.com/primandproper/platform-go/v9/observability/metrics"
	"github.com/primandproper/platform-go/v9/observability/tracing"

	"github.com/samber/do/v2"
)

// RegisterMultiSourceEventReporter registers a *MultiSourceEventReporter with the injector.
// Prerequisite: map[string]*analyticscfg.SourceConfig must be registered in the injector.
func RegisterMultiSourceEventReporter(i do.Injector) {
	do.Provide[*MultiSourceEventReporter](i, func(i do.Injector) (*MultiSourceEventReporter, error) {
		return NewMultiSourceEventReporterFromConfig(
			do.MustInvoke[context.Context](i),
			do.MustInvoke[map[string]*analyticscfg.SourceConfig](i),
			WithLogger(do.MustInvoke[logging.Logger](i)),
			WithTracerProvider(do.MustInvoke[tracing.TracerProvider](i)),
			WithMetricsProvider(do.MustInvoke[metrics.Provider](i)),
		)
	})
}
