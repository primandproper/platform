package indexing

import (
	"context"

	"github.com/primandproper/platform/messagequeue"
	msgconfig "github.com/primandproper/platform/messagequeue/config"
	"github.com/primandproper/platform/observability/logging"
	"github.com/primandproper/platform/observability/metrics"
	"github.com/primandproper/platform/observability/tracing"

	"github.com/samber/do/v2"
)

// RegisterIndexScheduler registers an *IndexScheduler with the injector.
// Prerequisites: map[string]Function and *msgconfig.QueuesConfig must be
// registered in the injector before calling this.
func RegisterIndexScheduler(i do.Injector) {
	do.Provide(i, func(i do.Injector) (*IndexScheduler, error) {
		return NewIndexScheduler(
			do.MustInvoke[context.Context](i),
			do.MustInvoke[logging.Logger](i),
			do.MustInvoke[tracing.TracerProvider](i),
			do.MustInvoke[metrics.Provider](i),
			do.MustInvoke[messagequeue.PublisherProvider](i),
			do.MustInvoke[*msgconfig.QueuesConfig](i),
			do.MustInvoke[map[string]Function](i),
		)
	})
}
