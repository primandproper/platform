package stripe

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/primandproper/platform-go/v14/capitalism"
	"github.com/primandproper/platform-go/v14/encoding"
	"github.com/primandproper/platform-go/v14/observability"
	loggingnoop "github.com/primandproper/platform-go/v14/observability/logging/noop"
	metricsnoop "github.com/primandproper/platform-go/v14/observability/metrics/noop"
	tracingnoop "github.com/primandproper/platform-go/v14/observability/tracing/noop"
	"github.com/primandproper/platform-go/v14/random"
	"github.com/primandproper/platform-go/v14/webhooks/inbound"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
	"github.com/stripe/stripe-go/v81"
	"github.com/stripe/stripe-go/v81/client"
	"github.com/stripe/stripe-go/v81/webhook"
)

type capturedRequest struct {
	form           url.Values
	method         string
	path           string
	idempotencyKey string
}

// newTestManager builds a stripePaymentManager whose Stripe client talks to an httptest server, so
// a test can drive the outbound operations and inspect the request Stripe would have sent. respond
// returns the (status, JSON body) for a given request path.
func newTestManager(t *testing.T, respond func(path string) (int, string)) (*PaymentManager, *[]capturedRequest) {
	t.Helper()

	var (
		mu       sync.Mutex
		captured []capturedRequest
	)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		mu.Lock()
		captured = append(captured, capturedRequest{
			method:         r.Method,
			path:           r.URL.Path,
			form:           r.Form,
			idempotencyKey: r.Header.Get("Idempotency-Key"),
		})
		mu.Unlock()

		status, body := respond(r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(ts.Close)

	backend := stripe.GetBackendWithConfig(stripe.APIBackend, &stripe.BackendConfig{URL: new(ts.URL)})
	sc := &client.API{}
	sc.Init("sk_test_123", &stripe.Backends{API: backend, Connect: backend, Uploads: backend})

	instruments, err := newInstruments(metricsnoop.NewMetricsProvider())
	must.NoError(t, err)

	pm := &PaymentManager{
		client:         sc,
		encoderDecoder: encoding.NewServerEncoderDecoder(encoding.ContentTypeJSON),
		o11y:           observability.NewObserver(implementationName, loggingnoop.NewLogger(), tracingnoop.NewTracerProvider()),
		instruments:    instruments,
	}

	return pm, &captured
}

func TestStripePaymentManager_CreatePaymentIntent(T *testing.T) {
	T.Parallel()

	T.Run("sends the correct request", func(t *testing.T) {
		t.Parallel()

		pm, captured := newTestManager(t, func(string) (int, string) {
			return http.StatusOK, `{"id":"pi_test","client_secret":"cs_test","object":"payment_intent"}`
		})

		result, err := pm.CreatePaymentIntent(t.Context(), &capitalism.PaymentIntentCreationInput{
			Amount:         1000,
			Currency:       "usd",
			CustomerID:     "cus_abc",
			Description:    "a widget",
			Metadata:       map[string]string{"order_id": "o-42"},
			IdempotencyKey: "idem-pi-1",
		})
		must.NoError(t, err)

		test.EqOp(t, "pi_test", result.ID)
		test.EqOp(t, "cs_test", result.ClientSecret)

		reqs := *captured
		must.SliceLen(t, 1, reqs)
		got := reqs[0]
		test.EqOp(t, http.MethodPost, got.method)
		test.EqOp(t, "/v1/payment_intents", got.path)
		test.EqOp(t, "1000", got.form.Get("amount"))
		test.EqOp(t, "usd", got.form.Get("currency"))
		test.EqOp(t, "cus_abc", got.form.Get("customer"))
		test.EqOp(t, "a widget", got.form.Get("description"))
		test.EqOp(t, "o-42", got.form.Get("metadata[order_id]"))
		test.EqOp(t, "idem-pi-1", got.idempotencyKey)
	})

	T.Run("errors without an API key", func(t *testing.T) {
		t.Parallel()

		pm, err := NewPaymentManager(&Config{WebhookSecret: "whsec"})
		must.NoError(t, err)

		result, err := pm.CreatePaymentIntent(t.Context(), &capitalism.PaymentIntentCreationInput{Amount: 1, Currency: "usd"})
		test.Error(t, err)
		test.Nil(t, result)
	})

	T.Run("errors on nil input", func(t *testing.T) {
		t.Parallel()

		pm, _ := newTestManager(t, func(string) (int, string) { return http.StatusOK, `{}` })

		result, err := pm.CreatePaymentIntent(t.Context(), nil)
		test.Error(t, err)
		test.Nil(t, result)
	})
}

func TestStripePaymentManager_CreateCustomer(T *testing.T) {
	T.Parallel()

	T.Run("sends the correct request", func(t *testing.T) {
		t.Parallel()

		pm, captured := newTestManager(t, func(string) (int, string) {
			return http.StatusOK, `{"id":"cus_test","object":"customer"}`
		})

		id, err := pm.CreateCustomer(t.Context(), &capitalism.CustomerCreationInput{
			Email:          "buyer@example.com",
			Name:           "Buyer Person",
			Metadata:       map[string]string{"tier": "gold"},
			IdempotencyKey: "idem-cus-1",
		})
		must.NoError(t, err)
		test.EqOp(t, "cus_test", id)

		reqs := *captured
		must.SliceLen(t, 1, reqs)
		got := reqs[0]
		test.EqOp(t, http.MethodPost, got.method)
		test.EqOp(t, "/v1/customers", got.path)
		test.EqOp(t, "buyer@example.com", got.form.Get("email"))
		test.EqOp(t, "Buyer Person", got.form.Get("name"))
		test.EqOp(t, "gold", got.form.Get("metadata[tier]"))
		test.EqOp(t, "idem-cus-1", got.idempotencyKey)
	})

	T.Run("errors without an API key", func(t *testing.T) {
		t.Parallel()

		pm, err := NewPaymentManager(&Config{WebhookSecret: "whsec"})
		must.NoError(t, err)

		id, err := pm.CreateCustomer(t.Context(), &capitalism.CustomerCreationInput{Email: "x@y.z"})
		test.Error(t, err)
		test.EqOp(t, "", id)
	})

	T.Run("errors on nil input", func(t *testing.T) {
		t.Parallel()

		pm, _ := newTestManager(t, func(string) (int, string) { return http.StatusOK, `{}` })

		id, err := pm.CreateCustomer(t.Context(), nil)
		test.Error(t, err)
		test.EqOp(t, "", id)
	})

	T.Run("errors when the Stripe API rejects the request", func(t *testing.T) {
		t.Parallel()

		pm, _ := newTestManager(t, func(string) (int, string) {
			return http.StatusBadRequest, `{"error":{"message":"boom","type":"invalid_request_error"}}`
		})

		id, err := pm.CreateCustomer(t.Context(), &capitalism.CustomerCreationInput{Email: "buyer@example.com"})
		test.Error(t, err)
		test.EqOp(t, "", id)
	})
}

func TestStripePaymentManager_CreateSubscription(T *testing.T) {
	T.Parallel()

	T.Run("sends the correct request", func(t *testing.T) {
		t.Parallel()

		pm, captured := newTestManager(t, func(string) (int, string) {
			return http.StatusOK, `{"id":"sub_test","object":"subscription"}`
		})

		id, err := pm.CreateSubscription(t.Context(), &capitalism.SubscriptionCreationInput{
			CustomerID:     "cus_abc",
			PriceID:        "price_xyz",
			IdempotencyKey: "idem-sub-1",
		})
		must.NoError(t, err)
		test.EqOp(t, "sub_test", id)

		reqs := *captured
		must.SliceLen(t, 1, reqs)
		got := reqs[0]
		test.EqOp(t, http.MethodPost, got.method)
		test.EqOp(t, "/v1/subscriptions", got.path)
		test.EqOp(t, "cus_abc", got.form.Get("customer"))
		test.EqOp(t, "price_xyz", got.form.Get("items[0][price]"))
		test.EqOp(t, "idem-sub-1", got.idempotencyKey)
	})

	T.Run("errors without an API key", func(t *testing.T) {
		t.Parallel()

		pm, err := NewPaymentManager(&Config{WebhookSecret: "whsec"})
		must.NoError(t, err)

		id, err := pm.CreateSubscription(t.Context(), &capitalism.SubscriptionCreationInput{CustomerID: "cus_abc", PriceID: "price_xyz"})
		test.Error(t, err)
		test.EqOp(t, "", id)
	})
}

func TestStripePaymentManager_HandleEventWebhook_ReturnsEvent(T *testing.T) {
	T.Parallel()

	signedRequest := func(t *testing.T, pm *PaymentManager, secret string, event *stripe.Event) *http.Request {
		t.Helper()

		ctx := t.Context()
		// Signed with stripe-go's own test helper, which is what makes this a cross-check:
		// the header comes from the SDK and the verification comes from webhooks/inbound, so
		// the two agreeing is evidence the extracted scheme is the same scheme.
		signed := webhook.GenerateTestSignedPayload(&webhook.UnsignedPayload{
			Payload:   pm.encoderDecoder.MustEncode(ctx, event),
			Secret:    secret,
			Timestamp: time.Now(),
		})

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://example.test/webhook", bytes.NewReader(signed.Payload))
		must.NoError(t, err)
		req.Header.Set(inbound.StripeSignatureHeader, signed.Header)

		return req
	}

	newManager := func(t *testing.T) (*PaymentManager, string) {
		t.Helper()

		secret, err := random.GenerateHexEncodedString(t.Context(), 32)
		must.NoError(t, err)

		pm, err := NewPaymentManager(&Config{WebhookSecret: secret})
		must.NoError(t, err)

		return pm, secret
	}

	T.Run("hands the verified event back to the caller", func(t *testing.T) {
		t.Parallel()

		pm, secret := newManager(t)

		req := signedRequest(t, pm, secret, &stripe.Event{
			APIVersion: stripeAPIVersion,
			ID:         "evt_test_123",
			Data:       &stripe.EventData{Raw: []byte(`{"id":"pi_1"}`)},
			Type:       stripe.EventTypePaymentIntentSucceeded,
		})

		// The whole point of the return: observing a delivery no longer takes a callback
		// registered with the constructor, and so no longer takes naming this package.
		event, err := pm.HandleEventWebhook(req)
		must.NoError(t, err)
		must.NotNil(t, event)

		test.EqOp(t, "evt_test_123", event.ID)
		test.EqOp(t, string(stripe.EventTypePaymentIntentSucceeded), event.Type)
		test.Eq(t, []byte(`{"id":"pi_1"}`), event.Payload)
	})

	T.Run("maps every subscription status Stripe reports", func(t *testing.T) {
		t.Parallel()

		// Every status stripe-go v81 declares, paired with what this module calls it. It is
		// spelled out rather than derived from the mapping table so that the table and the
		// expectation cannot drift together into agreeing on the wrong thing.
		for _, tc := range []struct {
			stripeStatus stripe.SubscriptionStatus
			want         capitalism.SubscriptionStatus
		}{
			{stripe.SubscriptionStatusIncomplete, capitalism.SubscriptionStatusIncomplete},
			{stripe.SubscriptionStatusIncompleteExpired, capitalism.SubscriptionStatusIncompleteExpired},
			{stripe.SubscriptionStatusTrialing, capitalism.SubscriptionStatusTrialing},
			{stripe.SubscriptionStatusActive, capitalism.SubscriptionStatusActive},
			{stripe.SubscriptionStatusPastDue, capitalism.SubscriptionStatusPastDue},
			{stripe.SubscriptionStatusCanceled, capitalism.SubscriptionStatusCanceled},
			{stripe.SubscriptionStatusUnpaid, capitalism.SubscriptionStatusUnpaid},
			{stripe.SubscriptionStatusPaused, capitalism.SubscriptionStatusPaused},
		} {
			t.Run(string(tc.stripeStatus), func(t *testing.T) {
				t.Parallel()

				pm, secret := newManager(t)

				req := signedRequest(t, pm, secret, subscriptionEvent(t, stripe.EventTypeCustomerSubscriptionUpdated, `{
					"id": "sub_123",
					"customer": "cus_123",
					"status": "`+string(tc.stripeStatus)+`"
				}`))

				event, err := pm.HandleEventWebhook(req)
				must.NoError(t, err)
				must.NotNil(t, event)
				must.NotNil(t, event.Subscription)

				test.EqOp(t, tc.want, event.Subscription.Status)
				test.True(t, event.Subscription.Status.Known())
				test.EqOp(t, string(tc.stripeStatus), event.Subscription.ProviderStatus)
				test.EqOp(t, "sub_123", event.Subscription.ID)
				test.EqOp(t, "cus_123", event.Subscription.CustomerID)
			})
		}
	})

	T.Run("reports a status it does not recognize as unknown", func(t *testing.T) {
		t.Parallel()

		pm, secret := newManager(t)

		req := signedRequest(t, pm, secret, subscriptionEvent(t, stripe.EventTypeCustomerSubscriptionUpdated, `{
			"id": "sub_123",
			"customer": "cus_123",
			"status": "gone_fishing"
		}`))

		event, err := pm.HandleEventWebhook(req)
		must.NoError(t, err)
		must.NotNil(t, event)
		must.NotNil(t, event.Subscription)

		// The delivery is genuine, so it is not rejected — but a status this module has
		// never seen must not arrive looking like one of the eight it has. A consumer
		// cutting off an account reads Known() before it reads Status.
		test.EqOp(t, capitalism.SubscriptionStatusUnknown, event.Subscription.Status)
		test.False(t, event.Subscription.Status.Known())
		test.EqOp(t, "gone_fishing", event.Subscription.ProviderStatus)
	})

	T.Run("reads a customer Stripe expanded into an object", func(t *testing.T) {
		t.Parallel()

		pm, secret := newManager(t)

		// The same field arrives as an ID or as the whole customer depending on how the
		// endpoint is configured, and a webhook handler does not get to choose which.
		req := signedRequest(t, pm, secret, subscriptionEvent(t, stripe.EventTypeCustomerSubscriptionCreated, `{
			"id": "sub_123",
			"customer": {"id": "cus_expanded", "object": "customer"},
			"status": "active"
		}`))

		event, err := pm.HandleEventWebhook(req)
		must.NoError(t, err)
		must.NotNil(t, event)
		must.NotNil(t, event.Subscription)

		test.EqOp(t, "cus_expanded", event.Subscription.CustomerID)
		test.EqOp(t, capitalism.SubscriptionStatusActive, event.Subscription.Status)
	})

	T.Run("reports a cancellation as the status on the deleted subscription", func(t *testing.T) {
		t.Parallel()

		pm, secret := newManager(t)

		req := signedRequest(t, pm, secret, subscriptionEvent(t, stripe.EventTypeCustomerSubscriptionDeleted, `{
			"id": "sub_123",
			"customer": "cus_123",
			"status": "canceled"
		}`))

		event, err := pm.HandleEventWebhook(req)
		must.NoError(t, err)
		must.NotNil(t, event)
		must.NotNil(t, event.Subscription)

		test.EqOp(t, capitalism.SubscriptionStatusCanceled, event.Subscription.Status)
	})

	T.Run("leaves the customer empty when the subscription names none", func(t *testing.T) {
		t.Parallel()

		pm, secret := newManager(t)

		req := signedRequest(t, pm, secret, subscriptionEvent(t, stripe.EventTypeCustomerSubscriptionUpdated, `{"id":"sub_123","status":"active"}`))

		event, err := pm.HandleEventWebhook(req)
		must.NoError(t, err)
		must.NotNil(t, event)
		must.NotNil(t, event.Subscription)

		// A verified delivery is still whatever its sender chose to send; the customer
		// pointer is not dereferenced on trust.
		test.EqOp(t, "", event.Subscription.CustomerID)
	})

	T.Run("errors on a subscription that does not decode", func(t *testing.T) {
		t.Parallel()

		pm, secret := newManager(t)

		req := signedRequest(t, pm, secret, subscriptionEvent(t, stripe.EventTypeCustomerSubscriptionUpdated, `["not","a","subscription"]`))

		event, err := pm.HandleEventWebhook(req)
		test.Error(t, err)
		test.Nil(t, event)
	})

	T.Run("keeps the raw payload for a consumer with its own stripe-go", func(t *testing.T) {
		t.Parallel()

		pm, secret := newManager(t)

		raw := `{"id":"sub_123","customer":"cus_123","status":"active","cancel_at_period_end":true}`
		req := signedRequest(t, pm, secret, subscriptionEvent(t, stripe.EventTypeCustomerSubscriptionUpdated, raw))

		event, err := pm.HandleEventWebhook(req)
		must.NoError(t, err)
		must.NotNil(t, event)

		// Everything richer than the status is still decodable by the caller, with whatever
		// stripe-go version it pins — which is why Payload stays bytes.
		var decoded stripe.Subscription
		must.NoError(t, json.Unmarshal(event.Payload, &decoded))
		test.True(t, decoded.CancelAtPeriodEnd)
	})
}

// subscriptionEvent builds a customer.subscription.* event carrying raw as its data object.
func subscriptionEvent(t *testing.T, eventType stripe.EventType, raw string) *stripe.Event {
	t.Helper()

	return &stripe.Event{
		APIVersion: stripeAPIVersion,
		ID:         "evt_sub_123",
		Data:       &stripe.EventData{Raw: json.RawMessage(raw)},
		Type:       eventType,
	}
}
