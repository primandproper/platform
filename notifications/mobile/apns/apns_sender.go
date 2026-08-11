package apns

import (
	"context"

	"github.com/primandproper/platform-go/v10/charset"
	"github.com/primandproper/platform-go/v10/errors"
	"github.com/primandproper/platform-go/v10/observability"
	"github.com/primandproper/platform-go/v10/observability/keys"
	"github.com/primandproper/platform-go/v10/observability/metrics"

	"github.com/sideshow/apns2"
	"github.com/sideshow/apns2/payload"
	"github.com/sideshow/apns2/token"
)

// apnsDeviceToken is a 64-character hex string, which is how APNs spells the
// 32-byte token. Either case: the token is minted by the device and arrives as
// it was given, so rejecting one for its case would refuse a value that is not
// wrong.
var apnsDeviceToken = charset.New(charset.HexDigits, charset.WithExactLength(64))

const (
	o11yName = "ios_notif_sender"
)

// Config holds APNs configuration.
type Config struct {
	AuthKeyPath string
	KeyID       string
	TeamID      string
	BundleID    string
	Production  bool
}

// Sender sends push notifications to iOS devices via APNs.
type Sender struct {
	o11y         observability.Observer
	client       *apns2.Client
	sendCounter  metrics.Int64Counter
	errorCounter metrics.Int64Counter
	topic        string
}

// NewSender creates an APNs sender from config.
func NewSender(cfg *Config, opts ...Option) (*Sender, error) {
	if cfg == nil || cfg.AuthKeyPath == "" || cfg.KeyID == "" || cfg.TeamID == "" || cfg.BundleID == "" {
		return nil, errors.New("apns: missing required config (authKeyPath, keyID, teamID, bundleID)")
	}

	o := newOptions(opts)

	authKey, err := token.AuthKeyFromFile(cfg.AuthKeyPath)
	if err != nil {
		return nil, errors.Wrap(err, "apns: loading auth key")
	}

	t := &token.Token{
		AuthKey: authKey,
		KeyID:   cfg.KeyID,
		TeamID:  cfg.TeamID,
	}
	if _, err = t.Generate(); err != nil {
		return nil, errors.Wrap(err, "apns: generating token")
	}

	client := apns2.NewTokenClient(t)
	if cfg.Production {
		client = client.Production()
	} else {
		client = client.Development()
	}

	mp := metrics.EnsureMetricsProvider(o.metricsProvider)

	sendCounter, err := mp.NewInt64Counter(o11yName + "_sends")
	if err != nil {
		return nil, errors.Wrap(err, "apns: creating send counter")
	}

	errorCounter, err := mp.NewInt64Counter(o11yName + "_errors")
	if err != nil {
		return nil, errors.Wrap(err, "apns: creating error counter")
	}

	return &Sender{
		client:       client,
		topic:        cfg.BundleID,
		o11y:         observability.NewObserver(o11yName, o.logger, o.tracerProvider),
		sendCounter:  sendCounter,
		errorCounter: errorCounter,
	}, nil
}

// Send sends a push notification to a single device token.
// The device token must be a 64-character hex string (APNs format).
// badgeCount is optional; when non-nil, sets aps.badge on the app icon.
func (s *Sender) Send(ctx context.Context, deviceToken, title, body string, badgeCount *int) error {
	ctx, op := s.o11y.Begin(ctx)
	defer op.End()

	if !apnsDeviceToken.Valid(deviceToken) {
		return op.Error(errors.Newf("apns: invalid device token format (expected 64 hex chars, got len %d)", len(deviceToken)), "validating device token")
	}

	op.Set("title", title)

	p := payload.NewPayload().
		AlertTitle(title).
		AlertBody(body)
	if badgeCount != nil {
		p = p.Badge(*badgeCount)
	}

	n := &apns2.Notification{
		DeviceToken: deviceToken,
		Topic:       s.topic,
		Payload:     p,
		Priority:    apns2.PriorityHigh,
	}

	res, err := s.client.PushWithContext(ctx, n)
	if err != nil {
		s.errorCounter.Add(ctx, 1)
		// The rejection path below goes through op.Error and this one used to
		// return a bare wrap, so the transport failures — the ones where APNs was
		// unreachable rather than unhappy — were the failures that left a green
		// span. The FCM sibling reports its equivalent through op.Error.
		return op.Error(err, "apns: push failed")
	}

	if !res.Sent() {
		s.errorCounter.Add(ctx, 1)
		err = errors.Newf("apns: %s (status %d)", res.Reason, res.StatusCode)
		op.Set("statusCode", res.StatusCode).
			Set(keys.ReasonKey, res.Reason).
			Set("apnsID", res.ApnsID)
		return op.Error(err, "sending apns notification")
	}

	op.Set("apnsID", res.ApnsID)

	s.sendCounter.Add(ctx, 1)
	return nil
}
