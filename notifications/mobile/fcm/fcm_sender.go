package fcm

import (
	"context"
	"os"

	"github.com/primandproper/platform-go/v13/errors"
	"github.com/primandproper/platform-go/v13/notifications/mobile/internal/pushfeedback"
	"github.com/primandproper/platform-go/v13/observability"
	"github.com/primandproper/platform-go/v13/observability/metrics"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/messaging"
	validation "github.com/go-ozzo/ozzo-validation/v4"
	"google.golang.org/api/option"
)

const (
	o11yName = "android_notif_sender"
)

// Config holds FCM configuration.
//
// An entirely empty Config is a valid one: it asks for Application Default
// Credentials, which is the normal way to run on GCP. Selecting FCM is what
// turns Android push on, not the presence of anything in here — which is why the
// validation below constrains nothing and exists so that a Config nested in a
// larger one is validated like every other node in the tree.
type Config struct {
	// CredentialsPath is the path to the Firebase service account JSON file.
	// If empty, Application Default Credentials (ADC) are used.
	CredentialsPath string `env:"CREDENTIALS_PATH" json:"credentialsPath,omitempty" yaml:"credentialsPath,omitempty"`
}

var _ validation.ValidatableWithContext = (*Config)(nil)

// ValidateWithContext validates a Config.
func (cfg *Config) ValidateWithContext(ctx context.Context) error {
	return validation.ValidateStructWithContext(ctx, cfg)
}

// Sender sends push notifications to Android devices via FCM.
type Sender struct {
	client       *messaging.Client
	o11y         observability.Observer
	sendCounter  metrics.Int64Counter
	errorCounter metrics.Int64Counter
}

// NewSender creates an FCM sender from config.
func NewSender(ctx context.Context, cfg *Config, opts ...Option) (*Sender, error) {
	if cfg == nil {
		return nil, errors.Wrap(errors.ErrNilInputParameter, "fcm: config is required")
	}

	if err := cfg.ValidateWithContext(ctx); err != nil {
		return nil, errors.Wrap(err, "fcm: validating config")
	}

	o := newOptions(opts)

	var clientOpts []option.ClientOption
	if cfg.CredentialsPath != "" {
		creds, err := os.ReadFile(cfg.CredentialsPath)
		if err != nil {
			return nil, errors.Wrap(err, "fcm: credentials file not found")
		}
		clientOpts = append(clientOpts, option.WithAuthCredentialsJSON(option.ServiceAccount, creds))
	}
	// If CredentialsPath is empty, Application Default Credentials (ADC) are used.

	app, err := firebase.NewApp(ctx, nil, clientOpts...)
	if err != nil {
		return nil, errors.Wrap(err, "fcm: initializing app")
	}

	client, err := app.Messaging(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "fcm: creating messaging client")
	}

	mp := metrics.EnsureMetricsProvider(o.metricsProvider)

	sendCounter, err := mp.NewInt64Counter(o11yName + "_sends")
	if err != nil {
		return nil, errors.Wrap(err, "fcm: creating send counter")
	}

	errorCounter, err := mp.NewInt64Counter(o11yName + "_errors")
	if err != nil {
		return nil, errors.Wrap(err, "fcm: creating error counter")
	}

	return &Sender{
		client:       client,
		o11y:         observability.NewObserver(o11yName, o.logger, o.tracerProvider),
		sendCounter:  sendCounter,
		errorCounter: errorCounter,
	}, nil
}

// Send sends a push notification to a single device token.
func (s *Sender) Send(ctx context.Context, deviceToken, title, body string) error {
	ctx, op := s.o11y.Begin(ctx, observability.WithValue("title", title))
	defer op.End()

	msg := &messaging.Message{
		Token: deviceToken,
		Notification: &messaging.Notification{
			Title: title,
			Body:  body,
		},
	}

	messageID, err := s.client.Send(ctx, msg)
	if err != nil {
		s.errorCounter.Add(ctx, 1)

		return op.Error(markDeadToken(err, tokenIsDead(err)), "sending fcm message")
	}

	op.Set("fcm.message_id", messageID)

	s.sendCounter.Add(ctx, 1)
	return nil
}

// tokenIsDead reports whether an FCM failure means the registration token will
// never accept another notification.
//
// Two of the SDK's classifications rather than every 4xx, and the line is the
// same one the APNs sibling draws: the token is what Firebase is refusing, not
// the request around it.
//
//   - UNREGISTERED: the app is gone from the device, or the token was reissued.
//   - SENDER_ID_MISMATCH: the token was minted for a different Firebase project.
//     It will never work here, whatever it does for whoever owns that project.
//
// INVALID_ARGUMENT is deliberately not among them, and it is the one worth
// naming. Firebase reports it for a malformed token and for a malformed message
// alike, so treating it as fatal to the token would empty a registry the first
// time somebody shipped a notification with a bad payload — every send would
// fail, and every token would be classified dead on the way past.
func tokenIsDead(err error) bool {
	return messaging.IsUnregistered(err) || messaging.IsSenderIDMismatch(err)
}

// markDeadToken marks a send failure with the sentinel a registry prunes on,
// leaving it alone when the token is not what Firebase refused.
//
// It takes the classification rather than making it, and that is what makes the
// marking testable at all: a messaging error carries its code in a struct only
// firebase's own internal package can build, so nothing outside the SDK can
// produce a genuine UNREGISTERED to send through here.
//
// Marked rather than replaced. The SDK's error is what says which of the two
// happened, and a caller reading the message wants it.
func markDeadToken(err error, dead bool) error {
	if err == nil || !dead {
		return err
	}

	return errors.Wrap(pushfeedback.ErrTokenInvalid, err.Error())
}
