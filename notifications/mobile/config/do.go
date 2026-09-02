package mobilecfg

import (
	"context"

	"github.com/primandproper/platform-go/v14/internal/injection"
	"github.com/primandproper/platform-go/v14/notifications"
	"github.com/primandproper/platform-go/v14/notifications/mobile"
	"github.com/primandproper/platform-go/v14/observability"

	"github.com/samber/do/v2"
)

// RegisterPushSender registers a mobile.PushNotificationSender with the injector.
//
// The notifications.Registry is resolved optionally, and it is the registration
// that closes this package's feedback loop. A provider answering a push with
// "this token is dead" raises mobile.ErrTokenInvalid, and a sender holding a
// registry deletes the row rather than addressing the same handset again
// tomorrow — see mobile.WithTokenInvalidator. A container that registers one,
// which notificationscfg.RegisterStore does, gets that pruning without wiring it
// by hand; a container that registers none gets exactly what it got before, with
// the classification still reaching the caller as an error and nothing acting on
// it.
//
// That is the same distinction the rest of this module's DI draws — absent is
// fine, registered-but-failing is an error.
//
// Prerequisites: Config must be registered in the injector before the sender is
// invoked.
func RegisterPushSender(i do.Injector) {
	do.Provide(i, func(i do.Injector) (mobile.PushNotificationSender, error) {
		pillars, err := observability.InvokePillars(i)
		if err != nil {
			return nil, err
		}

		registry, err := injection.InvokeOptional[notifications.Registry](i)
		if err != nil {
			return nil, err
		}

		opts := []Option{WithPillars(pillars)}
		if registry != nil {
			opts = append(opts, WithSenderOptions(mobile.WithTokenInvalidator(registry)))
		}

		return NewPushSender(
			do.MustInvoke[context.Context](i),
			do.MustInvoke[Config](i),
			opts...,
		)
	})
}
