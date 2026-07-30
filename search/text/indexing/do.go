package indexing

import (
	"context"

	"github.com/primandproper/platform-go/v8/messagequeue"
	msgconfig "github.com/primandproper/platform-go/v8/messagequeue/config"
	"github.com/primandproper/platform-go/v8/observability/logging"
	"github.com/primandproper/platform-go/v8/observability/metrics"
	"github.com/primandproper/platform-go/v8/observability/tracing"

	"github.com/samber/do/v2"
)

// RegisterIndexScheduler registers an *IndexScheduler with the injector.
// Prerequisites: map[string]Function and *msgconfig.QueuesConfig must be
// registered in the injector before calling this.
func RegisterIndexScheduler(i do.Injector) {
	do.Provide(i, func(i do.Injector) (*IndexScheduler, error) {
		return NewIndexScheduler(
			do.MustInvoke[context.Context](i),
			do.MustInvoke[messagequeue.PublisherProvider](i),
			do.MustInvoke[*msgconfig.QueuesConfig](i),
			do.MustInvoke[map[string]Function](i),
			WithLogger(do.MustInvoke[logging.Logger](i)),
			WithTracerProvider(do.MustInvoke[tracing.TracerProvider](i)),
			WithMetricsProvider(do.MustInvoke[metrics.Provider](i)),
		)
	})
}
