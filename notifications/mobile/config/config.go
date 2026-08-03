package mobilecfg

import (
	"context"
	"strings"

	"github.com/primandproper/platform-go/v9/errors"
	"github.com/primandproper/platform-go/v9/internal/cfgnorm"
	"github.com/primandproper/platform-go/v9/notifications/mobile"
	"github.com/primandproper/platform-go/v9/notifications/mobile/apns"
	"github.com/primandproper/platform-go/v9/notifications/mobile/fcm"
	"github.com/primandproper/platform-go/v9/notifications/mobile/noop"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

const (
	// ProviderAPNs sends to iOS only.
	ProviderAPNs = "apns"
	// ProviderFCM sends to Android only.
	ProviderFCM = "fcm"
	// ProviderAPNsFCM sends to both iOS and Android.
	ProviderAPNsFCM = "apns_fcm"
	// ProviderNoop represents the no-op implementation, which reports every
	// SendPush as a success. It must be selected deliberately — an unset or
	// typo'd provider is an error, because a push sender that silently succeeds
	// without sending anything is only noticed by the users who got no push.
	ProviderNoop = "noop"
)

type (
	// APNsConfig configures APNs for iOS push notifications.
	APNsConfig struct {
		AuthKeyPath string `env:"AUTH_KEY_PATH" json:"authKeyPath,omitempty" yaml:"authKeyPath,omitempty"`
		KeyID       string `env:"KEY_ID"        json:"keyID,omitempty"       yaml:"keyID,omitempty"`
		TeamID      string `env:"TEAM_ID"       json:"teamID,omitempty"      yaml:"teamID,omitempty"`
		BundleID    string `env:"BUNDLE_ID"     json:"bundleID,omitempty"    yaml:"bundleID,omitempty"`
		Production  bool   `env:"PRODUCTION"    json:"production,omitempty"  yaml:"production,omitempty"`
	}

	// FCMConfig configures FCM for Android push notifications.
	//
	// An entirely empty FCMConfig is a valid one: it asks for Application Default
	// Credentials, which is the normal way to run on GCP. Selecting FCM is what
	// turns Android push on, not the presence of anything in here.
	FCMConfig struct {
		// CredentialsPath is the path to the Firebase service account JSON file.
		// If empty, Application Default Credentials (ADC) are used.
		CredentialsPath string `env:"CREDENTIALS_PATH" json:"credentialsPath,omitempty" yaml:"credentialsPath,omitempty"`
	}

	// Config is the push notifications configuration.
	Config struct {
		APNs     *APNsConfig `env:",init"    envPrefix:"APNS_"         json:"apns,omitempty"     yaml:"apns,omitempty"`
		FCM      *FCMConfig  `env:",init"    envPrefix:"FCM_"          json:"fcm,omitempty"      yaml:"fcm,omitempty"`
		Provider string      `env:"PROVIDER" json:"provider,omitempty" yaml:"provider,omitempty"`
	}
)

var _ validation.ValidatableWithContext = (*Config)(nil)

var _ validation.ValidatableWithContext = (*APNsConfig)(nil)

// ValidateWithContext validates the APNsConfig. APNs has no ambient-credential
// equivalent of FCM's ADC, so every one of these has to be supplied.
func (cfg *APNsConfig) ValidateWithContext(ctx context.Context) error {
	return validation.ValidateStructWithContext(ctx, cfg,
		validation.Field(&cfg.AuthKeyPath, validation.Required),
		validation.Field(&cfg.KeyID, validation.Required),
		validation.Field(&cfg.TeamID, validation.Required),
		validation.Field(&cfg.BundleID, validation.Required),
	)
}

// ValidateWithContext validates the Config.
//
// The provider is the gate: it names which platforms are on, and each named one
// must be usable. Presence of a sub-config decides nothing, which is what lets an
// empty FCM block mean "use Application Default Credentials" rather than
// "Android is off".
func (cfg *Config) ValidateWithContext(ctx context.Context) error {
	provider := strings.ToLower(strings.TrimSpace(cfg.Provider))

	// Release an APNs block env parsing's ",init" allocated and nothing filled in,
	// so a deployment that selected FCM alone is not asked for iOS credentials by
	// the APNsConfig validation below.
	cfgnorm.ZeroToNil(&cfg.APNs)

	return validation.ValidateStructWithContext(
		ctx,
		cfg,
		validation.Field(&cfg.Provider, validation.Required, validation.In(ProviderAPNs, ProviderFCM, ProviderAPNsFCM, ProviderNoop)),
		validation.Field(&cfg.APNs, validation.When(
			provider == ProviderAPNs || provider == ProviderAPNsFCM,
			validation.Required,
		)),
	)
}

// NewPushSender returns a PushNotificationSender based on config.
//
// The provider names the platforms, and each one it names must initialize
// successfully; a failed init surfaces as an error rather than silently degrading
// to a noop that would report every SendPush as a success.
func (cfg *Config) NewPushSender(
	ctx context.Context,
	opts ...Option,
) (mobile.PushNotificationSender, error) {
	o := newOptions(opts)
	logger, tracerProvider, metricsProvider := o.logger, o.tracerProvider, o.metricsProvider

	provider := strings.ToLower(strings.TrimSpace(cfg.Provider))

	switch provider {
	case ProviderAPNs, ProviderFCM, ProviderAPNsFCM:
		var apnsSender *apns.Sender
		if provider == ProviderAPNs || provider == ProviderAPNsFCM {
			if cfg.APNs == nil {
				return nil, errors.Newf("push notification provider %q selected with no APNs config", provider)
			}

			apnsCfg := &apns.Config{
				AuthKeyPath: cfg.APNs.AuthKeyPath,
				KeyID:       cfg.APNs.KeyID,
				TeamID:      cfg.APNs.TeamID,
				BundleID:    cfg.APNs.BundleID,
				Production:  cfg.APNs.Production,
			}
			s, err := apns.NewSender(apnsCfg, apns.WithTracerProvider(tracerProvider), apns.WithLogger(logger), apns.WithMetricsProvider(metricsProvider))
			if err != nil {
				return nil, errors.Wrap(err, "initializing APNs sender")
			}
			apnsSender = s
		}

		var fcmSender *fcm.Sender
		if provider == ProviderFCM || provider == ProviderAPNsFCM {
			// A nil or empty FCM block asks for Application Default Credentials,
			// so there is nothing to require here.
			fcmCfg := &fcm.Config{}
			if cfg.FCM != nil {
				fcmCfg.CredentialsPath = cfg.FCM.CredentialsPath
			}

			s, err := fcm.NewSender(ctx, fcmCfg, fcm.WithTracerProvider(tracerProvider), fcm.WithLogger(logger), fcm.WithMetricsProvider(metricsProvider))
			if err != nil {
				return nil, errors.Wrap(err, "initializing FCM sender")
			}
			fcmSender = s
		}

		return mobile.NewMultiPlatformPushSender(apnsSender, fcmSender, mobile.WithLogger(logger), mobile.WithTracerProvider(tracerProvider)), nil
	case ProviderNoop:
		return noop.NewPushNotificationSender(), nil
	default:
		return nil, errors.Wrapf(errors.ErrUnknownProvider, "push notification provider %q", cfg.Provider)
	}
}
