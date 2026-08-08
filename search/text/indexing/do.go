package indexing

import (
	"context"

	"github.com/primandproper/platform-go/v10/messagequeue"
	"github.com/primandproper/platform-go/v10/observability/logging"
	"github.com/primandproper/platform-go/v10/observability/metrics"
	"github.com/primandproper/platform-go/v10/observability/tracing"

	"github.com/samber/do/v2"
)

// RegisterIndexScheduler registers an *IndexScheduler with the injector.
//
// Prerequisites: map[string]Function must be registered in the injector before
// calling this. The topic is passed here rather than injected, because string is
// too generic a type to resolve unambiguously.
func RegisterIndexScheduler(i do.Injector, topic string) {
	do.Provide(i, func(i do.Injector) (*IndexScheduler, error) {
		return NewIndexScheduler(
			do.MustInvoke[context.Context](i),
			do.MustInvoke[messagequeue.PublisherProvider](i),
			topic,
			do.MustInvoke[map[string]Function](i),
			WithLogger(do.MustInvoke[logging.Logger](i)),
			WithTracerProvider(do.MustInvoke[tracing.Provider](i)),
			WithMetricsProvider(do.MustInvoke[metrics.Provider](i)),
		)
	})
}
