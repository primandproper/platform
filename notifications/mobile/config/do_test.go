package mobilecfg

import (
	"context"
	"testing"

	"github.com/primandproper/platform-go/v14/errors"
	"github.com/primandproper/platform-go/v14/notifications/mobile"
	"github.com/primandproper/platform-go/v14/notifications/mobile/apns"
	loggingnoop "github.com/primandproper/platform-go/v14/observability/logging/noop"
	"github.com/primandproper/platform-go/v14/observability/metrics"
	tracingnoop "github.com/primandproper/platform-go/v14/observability/tracing/noop"

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

	T.Run("a registered invalidator becomes the sender's token invalidator", func(t *testing.T) {
		t.Parallel()

		// The cross-wiring: notificationscfg.RegisterStore registers its store
		// under this key too, and finding one here is what closes the feedback
		// loop without either wiring site naming the other.
		//
		// The stand-in is declared here rather than taken from
		// notifications/mock, and that is the point rather than an omission:
		// the whole subject of this case is that the sender depends on one
		// method and not on the package that owns the device table. A test that
		// reached for the registry's mock would be reintroducing the import
		// this registration exists to have dropped.
		registry := &recordingInvalidator{}

		i := do.New()
		do.ProvideValue[context.Context](i, t.Context())
		do.ProvideValue[mobile.TokenInvalidator](i, registry)
		do.ProvideValue(i, Config{Provider: ProviderAPNs, APNs: apnsConfigForTest(t)})

		RegisterPushSender(i)

		sender, err := do.Invoke[mobile.PushNotificationSender](i)
		must.NoError(t, err)

		multi, ok := sender.(*mobile.MultiPlatformPushSender)
		must.True(t, ok)
		test.True(t, multi.TokenInvalidator() == mobile.TokenInvalidator(registry))
	})

	T.Run("no registered invalidator leaves the sender as it was", func(t *testing.T) {
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

	T.Run("a registered invalidator that fails to build is an error", func(t *testing.T) {
		t.Parallel()

		// Absent is fine; registered-but-failing is not. Treating the second as
		// the first would hand a deployment that configured a store a sender
		// that quietly prunes nothing.
		errBuild := errors.New("building the registry")

		i := do.New()
		do.ProvideValue[context.Context](i, t.Context())
		do.Provide(i, func(do.Injector) (mobile.TokenInvalidator, error) { return nil, errBuild })
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

// recordingInvalidator is the whole interface a sender needs of a device
// registry: two strings and an error.
type recordingInvalidator struct {
	platforms []string
	tokens    []string
}

var _ mobile.TokenInvalidator = (*recordingInvalidator)(nil)

func (i *recordingInvalidator) InvalidateDeviceToken(_ context.Context, platform, token string) error {
	i.platforms = append(i.platforms, platform)
	i.tokens = append(i.tokens, token)

	return nil
}

// apnsConfigForTest is enough iOS credentials to build a real
// MultiPlatformPushSender offline, which is the only provider whose product can
// be asked what it was wired with.
func apnsConfigForTest(t *testing.T) *apns.Config {
	t.Helper()

	return &apns.Config{AuthKeyPath: createTestP8File(t), KeyID: "K1", TeamID: "T1", BundleID: "com.example.app"}
}
