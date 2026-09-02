package capitalismcfg

import (
	"context"

	"github.com/primandproper/platform-go/v14/capitalism"
	"github.com/primandproper/platform-go/v14/observability"

	"github.com/samber/do/v2"
)

// RegisterPaymentManager registers a capitalism.PaymentManager with the injector.
//
// Nothing optional is resolved out of the container alongside it. The manager used to look
// for a registered stripe.EventHandler, which made acting on a webhook depend on a type from
// the provider subpackage and on a registration whose absence was silent;
// PaymentManager.HandleEventWebhook now returns the verified capitalism.Event to whoever
// mounted the endpoint.
func RegisterPaymentManager(i do.Injector) {
	do.Provide(i, func(i do.Injector) (capitalism.PaymentManager, error) {
		pillars, err := observability.InvokePillars(i)
		if err != nil {
			return nil, err
		}

		return NewPaymentManager(
			do.MustInvoke[context.Context](i),
			do.MustInvoke[*Config](i),
			WithPillars(pillars),
		)
	})
}

// RegisterUsageReporter registers a capitalism.UsageReporter with the injector.
//
// It is a separate registration from RegisterPaymentManager because the two are
// wanted by different processes: an API server charges, and a worker reports
// usage. A deployment registers whichever of the two it actually runs.
func RegisterUsageReporter(i do.Injector) {
	do.Provide(i, func(i do.Injector) (capitalism.UsageReporter, error) {
		pillars, err := observability.InvokePillars(i)
		if err != nil {
			return nil, err
		}

		return NewUsageReporter(
			do.MustInvoke[context.Context](i),
			do.MustInvoke[*Config](i),
			WithPillars(pillars),
		)
	})
}
