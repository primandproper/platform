package revenuecat

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/primandproper/platform-go/v13/capitalism"
	"github.com/primandproper/platform-go/v13/cryptography/hashing"
	"github.com/primandproper/platform-go/v13/cryptography/hashing/hmac"
	"github.com/primandproper/platform-go/v13/observability"
	"github.com/primandproper/platform-go/v13/webhooks/inbound"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// webhookSecret is what every test here signs under.
const webhookSecret = "rcsec_test"

type errReader struct{}

func (*errReader) Read([]byte) (int, error) { return 0, fmt.Errorf("read error") }
func (*errReader) Close() error             { return nil }

// signedRequest builds a request carrying body, signed under webhookSecret the
// way RevenueCat signs.
//
// The header is rendered here rather than taken from the verifier, which is what
// makes these tests a cross-check rather than a tautology: this is RevenueCat's
// documented scheme spelled out independently, and webhooks/inbound verifying it
// is the evidence the two agree.
func signedRequest(t *testing.T, body string) *http.Request {
	t.Helper()

	seconds := fmt.Sprintf("%d", time.Now().Unix())
	mac := hashing.HexString(hmac.NewHMACSHA256Hasher([]byte(webhookSecret)), seconds+"."+body)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://whatever.whocares.gov", strings.NewReader(body))
	must.NoError(t, err)
	req.Header.Set(inbound.RevenueCatSignatureHeader, "t="+seconds+",v1="+mac)

	return req
}

// delivery renders a RevenueCat webhook envelope around the given event fields.
func delivery(eventFields string) string {
	return `{"api_version":"1.0","event":{` + eventFields + `}}`
}

// newManager builds a manager whose verifier trusts webhookSecret.
func newManager(t *testing.T) *PaymentManager {
	t.Helper()

	manager, err := NewPaymentManager(&Config{WebhookSecret: webhookSecret})
	must.NoError(t, err)

	return manager
}

func TestNewPaymentManager(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		pm, err := NewPaymentManager(&Config{WebhookSecret: webhookSecret})

		must.NoError(t, err)
		test.NotNil(t, pm)
	})

	T.Run("nil config", func(t *testing.T) {
		t.Parallel()

		pm, err := NewPaymentManager(nil)

		test.ErrorIs(t, err, ErrNilConfig)
		test.Nil(t, pm)
	})

	T.Run("refuses to build without a webhook secret", func(t *testing.T) {
		t.Parallel()

		// There is no outbound half for a secretless manager to still serve, so it
		// could do nothing at all — and every delivery would come back a signature
		// error, which reads as RevenueCat's fault rather than a missing variable.
		pm, err := NewPaymentManager(&Config{})

		test.ErrorIs(t, err, ErrWebhookSecretNotConfigured)
		test.Nil(t, pm)
	})

	T.Run("ignores a nil option", func(t *testing.T) {
		t.Parallel()

		pm, err := NewPaymentManager(&Config{WebhookSecret: webhookSecret}, nil, WithLogger(nil), WithTracerProvider(nil), WithMetricsProvider(nil))

		must.NoError(t, err)
		test.NotNil(t, pm)
	})
}

func TestPaymentManager_HandleEventWebhook(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		pm := newManager(t)

		obs := observability.NewRecordingObserver()
		pm.o11y = obs

		event, err := pm.HandleEventWebhook(signedRequest(t, delivery(
			`"type":"RENEWAL","id":"evt_rc","app_user_id":"user_123","transaction_id":"txn_2","original_transaction_id":"txn_1","product_id":"monthly","period_type":"NORMAL"`,
		)))
		must.NoError(t, err)
		must.NotNil(t, event)

		test.EqOp(t, "evt_rc", event.ID)
		test.EqOp(t, EventTypeRenewal, event.Type)

		must.NotNil(t, event.Subscription)
		test.EqOp(t, "txn_1", event.Subscription.ID)
		test.EqOp(t, "user_123", event.Subscription.CustomerID)
		test.EqOp(t, capitalism.SubscriptionStatusActive, event.Subscription.Status)
		// RevenueCat has no status field, so the event type is what it spelled.
		test.EqOp(t, EventTypeRenewal, event.Subscription.ProviderStatus)

		obs.ObservedOperationWithData(t, map[string]any{
			"revenuecat.event_id":            "evt_rc",
			"revenuecat.event_type":          EventTypeRenewal,
			"revenuecat.subscription_id":     "txn_1",
			"revenuecat.app_user_id":         "user_123",
			"capitalism.subscription_status": "active",
		})
	})

	T.Run("hands back the event object exactly as it arrived", func(t *testing.T) {
		t.Parallel()

		// Payload is what a consumer decodes the rest of the event out of, with
		// whatever library it likes. Re-encoding a struct this package chose the
		// fields of would silently drop everything it did not name.
		pm := newManager(t)

		body := delivery(`"type":"RENEWAL","id":"evt_rc","store":"APP_STORE","price":9.99`)

		event, err := pm.HandleEventWebhook(signedRequest(t, body))
		must.NoError(t, err)
		must.NotNil(t, event)

		test.StrContains(t, string(event.Payload), `"store":"APP_STORE"`)
		test.StrContains(t, string(event.Payload), `"price":9.99`)
	})

	T.Run("folds a trial purchase onto trialing", func(t *testing.T) {
		t.Parallel()

		pm := newManager(t)

		event, err := pm.HandleEventWebhook(signedRequest(t, delivery(
			`"type":"INITIAL_PURCHASE","id":"evt_trial","app_user_id":"user_123","transaction_id":"txn_1","period_type":"TRIAL"`,
		)))
		must.NoError(t, err)
		must.NotNil(t, event.Subscription)

		test.EqOp(t, capitalism.SubscriptionStatusTrialing, event.Subscription.Status)
		// The provider's word is still the event type: RevenueCat did not say
		// "trialing", it said INITIAL_PURCHASE and put the trial in period_type.
		test.EqOp(t, EventTypeInitialPurchase, event.Subscription.ProviderStatus)
	})

	T.Run("keeps a cancelled subscriber entitled until it expires", func(t *testing.T) {
		t.Parallel()

		pm := newManager(t)

		event, err := pm.HandleEventWebhook(signedRequest(t, delivery(
			`"type":"CANCELLATION","id":"evt_cancel","app_user_id":"user_123","transaction_id":"txn_1","cancel_reason":"UNSUBSCRIBE"`,
		)))
		must.NoError(t, err)
		must.NotNil(t, event.Subscription)

		test.EqOp(t, capitalism.SubscriptionStatusActive, event.Subscription.Status)
	})

	T.Run("ends access for a refunded cancellation", func(t *testing.T) {
		t.Parallel()

		pm := newManager(t)

		event, err := pm.HandleEventWebhook(signedRequest(t, delivery(
			`"type":"CANCELLATION","id":"evt_refund","app_user_id":"user_123","transaction_id":"txn_1","cancel_reason":"CUSTOMER_SUPPORT"`,
		)))
		must.NoError(t, err)
		must.NotNil(t, event.Subscription)

		test.EqOp(t, capitalism.SubscriptionStatusCanceled, event.Subscription.Status)
	})

	T.Run("reports no subscription for an event that carries none", func(t *testing.T) {
		t.Parallel()

		pm := newManager(t)

		for _, eventType := range documentedSilences {
			event, err := pm.HandleEventWebhook(signedRequest(t, delivery(
				`"type":"`+eventType+`","id":"evt_quiet"`,
			)))
			must.NoError(t, err, must.Sprintf("event type %q", eventType))
			must.NotNil(t, event, must.Sprintf("event type %q", eventType))

			test.EqOp(t, eventType, event.Type, test.Sprintf("event type %q", eventType))
			// Nil rather than an unknown status: this package knows these carry no
			// standing, and reporting one it could not place would send a consumer
			// looking for a mapping-table entry that should not exist.
			test.Nil(t, event.Subscription, test.Sprintf("event type %q", eventType))
		}
	})

	T.Run("reports an event type it has never seen without guessing", func(t *testing.T) {
		t.Parallel()

		pm := newManager(t)

		event, err := pm.HandleEventWebhook(signedRequest(t, delivery(
			`"type":"GONE_FISHING","id":"evt_new","app_user_id":"user_123","transaction_id":"txn_1"`,
		)))
		must.NoError(t, err)
		must.NotNil(t, event)

		// Accepted, not refused: the delivery is genuine, and RevenueCat will stop
		// retrying it eventually. The unknown status plus the raw event type is what
		// makes the missing table entry diagnosable.
		must.NotNil(t, event.Subscription)
		test.EqOp(t, capitalism.SubscriptionStatusUnknown, event.Subscription.Status)
		test.False(t, event.Subscription.Status.Known())
		test.EqOp(t, "GONE_FISHING", event.Subscription.ProviderStatus)
		test.EqOp(t, "user_123", event.Subscription.CustomerID)
	})

	T.Run("falls back to the current transaction when there is no original", func(t *testing.T) {
		t.Parallel()

		pm := newManager(t)

		event, err := pm.HandleEventWebhook(signedRequest(t, delivery(
			`"type":"NON_RENEWING_PURCHASE","id":"evt_once","app_user_id":"user_123","transaction_id":"txn_only"`,
		)))
		must.NoError(t, err)
		must.NotNil(t, event.Subscription)

		test.EqOp(t, "txn_only", event.Subscription.ID)
	})

	T.Run("falls back to the original app user ID", func(t *testing.T) {
		t.Parallel()

		pm := newManager(t)

		event, err := pm.HandleEventWebhook(signedRequest(t, delivery(
			`"type":"RENEWAL","id":"evt_rc","original_app_user_id":"user_original","transaction_id":"txn_1"`,
		)))
		must.NoError(t, err)
		must.NotNil(t, event.Subscription)

		test.EqOp(t, "user_original", event.Subscription.CustomerID)
	})

	T.Run("rejects a delivery signed under another secret", func(t *testing.T) {
		t.Parallel()

		pm, err := NewPaymentManager(&Config{WebhookSecret: "rcsec_other"})
		must.NoError(t, err)

		event, err := pm.HandleEventWebhook(signedRequest(t, delivery(`"type":"RENEWAL","id":"evt_rc"`)))

		test.ErrorIs(t, err, inbound.ErrInvalidSignature)
		test.Nil(t, event)
	})

	T.Run("rejects an unsigned delivery", func(t *testing.T) {
		t.Parallel()

		pm := newManager(t)

		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://whatever.whocares.gov", strings.NewReader(delivery(`"type":"RENEWAL"`)))
		must.NoError(t, err)

		event, err := pm.HandleEventWebhook(req)

		test.ErrorIs(t, err, inbound.ErrInvalidSignature)
		test.Nil(t, event)
	})

	T.Run("rejects a body that was not the one signed", func(t *testing.T) {
		t.Parallel()

		pm := newManager(t)

		req := signedRequest(t, delivery(`"type":"RENEWAL","id":"evt_rc"`))
		req.Body = http.NoBody

		event, err := pm.HandleEventWebhook(req)

		test.ErrorIs(t, err, inbound.ErrInvalidSignature)
		test.Nil(t, event)
	})

	T.Run("with error reading body", func(t *testing.T) {
		t.Parallel()

		pm := newManager(t)

		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://whatever.whocares.gov", http.NoBody)
		must.NoError(t, err)
		req.Body = &errReader{}

		event, err := pm.HandleEventWebhook(req)

		test.Error(t, err)
		test.Nil(t, event)
	})

	T.Run("with oversized body", func(t *testing.T) {
		t.Parallel()

		pm := newManager(t)

		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://whatever.whocares.gov", bytes.NewReader(bytes.Repeat([]byte("a"), maxWebhookBodyBytes+1)))
		must.NoError(t, err)

		// Refused before verification, which is the point: the cap is what stands
		// between a public endpoint and an allocation a hostile client names.
		event, err := pm.HandleEventWebhook(req)

		test.Error(t, err)
		test.Nil(t, event)
	})

	T.Run("with an undecodable envelope", func(t *testing.T) {
		t.Parallel()

		pm := newManager(t)

		event, err := pm.HandleEventWebhook(signedRequest(t, `{"api_version":`))

		test.Error(t, err)
		test.Nil(t, event)
	})

	T.Run("with an undecodable event object", func(t *testing.T) {
		t.Parallel()

		pm := newManager(t)

		// The envelope parses and the event does not, which is a different failure
		// from the one above and reaches a different decode.
		event, err := pm.HandleEventWebhook(signedRequest(t, `{"api_version":"1.0","event":["not","an","object"]}`))

		test.Error(t, err)
		test.Nil(t, event)
	})

	T.Run("with a delivery carrying no event object", func(t *testing.T) {
		t.Parallel()

		pm := newManager(t)

		// A delivery signed under the right secret can still be shaped however its
		// sender liked. An absent event object is an event type of "", which is not
		// one this package recognizes.
		event, err := pm.HandleEventWebhook(signedRequest(t, `{"api_version":"1.0"}`))
		must.NoError(t, err)
		must.NotNil(t, event)

		test.EqOp(t, "", event.Type)
		must.NotNil(t, event.Subscription)
		test.EqOp(t, capitalism.SubscriptionStatusUnknown, event.Subscription.Status)
	})
}

func TestPaymentManager_outbound(T *testing.T) {
	T.Parallel()

	T.Run("reports every outbound operation as unsupported", func(t *testing.T) {
		t.Parallel()

		pm := newManager(t)
		ctx := t.Context()

		customerID, err := pm.CreateCustomer(ctx, &capitalism.CustomerCreationInput{Email: "somebody@example.com"})
		test.ErrorIs(t, err, ErrOutboundUnsupported)
		// Empty and an error, never empty and nil: an empty ID returned as a success
		// is what a caller persists and then finds pointing at nothing.
		test.EqOp(t, "", customerID)

		intent, err := pm.CreatePaymentIntent(ctx, &capitalism.PaymentIntentCreationInput{Amount: 4200, Currency: "usd"})
		test.ErrorIs(t, err, ErrOutboundUnsupported)
		test.Nil(t, intent)

		subscriptionID, err := pm.CreateSubscription(ctx, &capitalism.SubscriptionCreationInput{CustomerID: "user_123", PriceID: "monthly"})
		test.ErrorIs(t, err, ErrOutboundUnsupported)
		test.EqOp(t, "", subscriptionID)
	})

	T.Run("reports them for a nil input too", func(t *testing.T) {
		t.Parallel()

		// The input is never read, so there is nothing to reject it for. Reporting a
		// nil-input error would suggest a filled-in one would have worked.
		pm := newManager(t)
		ctx := t.Context()

		_, err := pm.CreateCustomer(ctx, nil)
		test.ErrorIs(t, err, ErrOutboundUnsupported)

		_, err = pm.CreatePaymentIntent(ctx, nil)
		test.ErrorIs(t, err, ErrOutboundUnsupported)

		_, err = pm.CreateSubscription(ctx, nil)
		test.ErrorIs(t, err, ErrOutboundUnsupported)
	})
}
