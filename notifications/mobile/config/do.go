package mobilecfg

import (
	"context"

	"github.com/primandproper/platform-go/v14/internal/injection"
	"github.com/primandproper/platform-go/v14/notifications/mobile"
	"github.com/primandproper/platform-go/v14/observability"

	"github.com/samber/do/v2"
)

// RegisterPushSender registers a mobile.PushNotificationSender with the injector.
//
// The mobile.TokenInvalidator is resolved optionally, and it is the registration
// that closes this package's feedback loop. A provider answering a push with
// "this token is dead" raises mobile.ErrTokenInvalid, and a sender holding an
// invalidator deletes the row rather than addressing the same handset again
// tomorrow — see mobile.WithTokenInvalidator. A container that registers one,
// which notificationscfg.RegisterStore does, gets that pruning without wiring it
// by hand; a container that registers none gets exactly what it got before, with
// the classification still reaching the caller as an error and nothing acting on
// it.
//
// The key is mobile.TokenInvalidator rather than the notifications.Registry that
// satisfies it, and that is the whole reason this package compiles without the
// store. An invalidator is one method taking two strings; a registry is five,
// over database.Tx, tenancy.Scope and a row type. Naming the wider one here
// would make every deployment that sends a push depend on the package that owns
// the device table, to reach a method the narrow interface already declares.
// notifications.Registry's own documentation says the plain-string signature
// exists so that a registry is wirable into a sender without either package
// importing the other; keying on the narrow interface is what finishes that
// sentence.
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

		invalidator, err := injection.InvokeOptional[mobile.TokenInvalidator](i)
		if err != nil {
			return nil, err
		}

		opts := []Option{WithPillars(pillars)}
		if invalidator != nil {
			opts = append(opts, WithSenderOptions(mobile.WithTokenInvalidator(invalidator)))
		}

		return NewPushSender(
			do.MustInvoke[context.Context](i),
			do.MustInvoke[Config](i),
			opts...,
		)
	})
}
