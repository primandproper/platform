package mobilecfg

import (
	"context"
	"testing"

	"github.com/primandproper/platform-go/v13/errors"
	"github.com/primandproper/platform-go/v13/notifications"
	"github.com/primandproper/platform-go/v13/notifications/mobile"
	"github.com/primandproper/platform-go/v13/notifications/mobile/apns"
	notificationsmock "github.com/primandproper/platform-go/v13/notifications/mock"
	loggingnoop "github.com/primandproper/platform-go/v13/observability/logging/noop"
	"github.com/primandproper/platform-go/v13/observability/metrics"
	tracingnoop "github.com/primandproper/platform-go/v13/observability/tracing/noop"

	"github.com/samber/do/v2"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestRegisterPushSender(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		i := do.New()
		do.ProvideValue[context.Context](i, t.Context())
		do.ProvideValue(i, loggingnoop.NewLogger())
		do.ProvideValue(i, tracingnoop.NewTracerProvider())
		do.ProvideValue[metrics.Provider](i, nil)
		do.ProvideValue(i, Config{Provider: ProviderNoop})

		RegisterPushSender(i)

		sender, err := do.Invoke[mobile.PushNotificationSender](i)
		must.NoError(t, err)
		test.NotNil(t, sender)
	})

	T.Run("a registered registry becomes the sender's token invalidator", func(t *testing.T) {
		t.Parallel()

		// The cross-wiring: notificationscfg.RegisterStore registers the
		// registry under this key, and finding one here is what closes the
		// feedback loop without either wiring site naming the other.
		registry := &notificationsmock.RegistryMock{}

		i := do.New()
		do.ProvideValue[context.Context](i, t.Context())
		do.ProvideValue[notifications.Registry](i, registry)
		do.ProvideValue(i, Config{Provider: ProviderAPNs, APNs: apnsConfigForTest(t)})

		RegisterPushSender(i)

		sender, err := do.Invoke[mobile.PushNotificationSender](i)
		must.NoError(t, err)

		multi, ok := sender.(*mobile.MultiPlatformPushSender)
		must.True(t, ok)
		test.True(t, multi.TokenInvalidator() == mobile.TokenInvalidator(registry))
	})

	T.Run("no registered registry leaves the sender as it was", func(t *testing.T) {
		t.Parallel()

		i := do.New()
		do.ProvideValue[context.Context](i, t.Context())
		do.ProvideValue(i, Config{Provider: ProviderAPNs, APNs: apnsConfigForTest(t)})

		RegisterPushSender(i)

		sender, err := do.Invoke[mobile.PushNotificationSender](i)
		must.NoError(t, err)

		multi, ok := sender.(*mobile.MultiPlatformPushSender)
		must.True(t, ok)
		test.Nil(t, multi.TokenInvalidator())
	})

	T.Run("a registered registry that fails to build is an error", func(t *testing.T) {
		t.Parallel()

		// Absent is fine; registered-but-failing is not. Treating the second as
		// the first would hand a deployment that configured a store a sender
		// that quietly prunes nothing.
		errBuild := errors.New("building the registry")

		i := do.New()
		do.ProvideValue[context.Context](i, t.Context())
		do.Provide(i, func(do.Injector) (notifications.Registry, error) { return nil, errBuild })
		do.ProvideValue(i, Config{Provider: ProviderNoop})

		RegisterPushSender(i)

		_, err := do.Invoke[mobile.PushNotificationSender](i)
		must.Error(t, err)
		test.ErrorIs(t, err, errBuild)
	})
}

func TestNewPushSender(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		sender, err := NewPushSender(
			t.Context(),
			Config{Provider: ProviderNoop},
			nil,
		)
		must.NoError(t, err)
		test.NotNil(t, sender)
	})
}

// apnsConfigForTest is enough iOS credentials to build a real
// MultiPlatformPushSender offline, which is the only provider whose product can
// be asked what it was wired with.
func apnsConfigForTest(t *testing.T) *apns.Config {
	t.Helper()

	return &apns.Config{AuthKeyPath: createTestP8File(t), KeyID: "K1", TeamID: "T1", BundleID: "com.example.app"}
}
