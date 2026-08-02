package eventstreamcfg

import (
	"context"

	"github.com/primandproper/platform-go/v9/eventstream"
	"github.com/primandproper/platform-go/v9/observability/logging"
	"github.com/primandproper/platform-go/v9/observability/tracing"

	"github.com/samber/do/v2"
)

// RegisterEventStreamUpgrader registers an eventstream.EventStreamUpgrader with the injector.
func RegisterEventStreamUpgrader(i do.Injector) {
	do.Provide[eventstream.EventStreamUpgrader](i, func(i do.Injector) (eventstream.EventStreamUpgrader, error) {
		return NewEventStreamUpgrader(
			do.MustInvoke[context.Context](i),
			do.MustInvoke[*Config](i),
			do.MustInvoke[logging.Logger](i),
			do.MustInvoke[tracing.TracerProvider](i),
		)
	})
}

// RegisterBidirectionalEventStreamUpgrader registers an eventstream.BidirectionalEventStreamUpgrader with the injector.
func RegisterBidirectionalEventStreamUpgrader(i do.Injector) {
	do.Provide[eventstream.BidirectionalEventStreamUpgrader](i, func(i do.Injector) (eventstream.BidirectionalEventStreamUpgrader, error) {
		return NewBidirectionalEventStreamUpgrader(
			do.MustInvoke[context.Context](i),
			do.MustInvoke[*Config](i),
			do.MustInvoke[logging.Logger](i),
			do.MustInvoke[tracing.TracerProvider](i),
		)
	})
}
