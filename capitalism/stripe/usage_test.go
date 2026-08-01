package stripe

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/primandproper/platform-go/v9/capitalism"
	platformerrors "github.com/primandproper/platform-go/v9/errors"
	"github.com/primandproper/platform-go/v9/observability"
	loggingnoop "github.com/primandproper/platform-go/v9/observability/logging/noop"
	tracingnoop "github.com/primandproper/platform-go/v9/observability/tracing/noop"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
	"github.com/stripe/stripe-go/v75"
	"github.com/stripe/stripe-go/v75/client"
)

// newTestUsageReporter builds a stripeUsageReporter whose Stripe client talks to
// an httptest server, so a test can drive the post and inspect the request Stripe
// would have received.
func newTestUsageReporter(t *testing.T, status int, body string) (*stripeUsageReporter, *[]capturedRequest) {
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

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(ts.Close)

	backend := stripe.GetBackendWithConfig(stripe.APIBackend, &stripe.BackendConfig{URL: new(ts.URL)})
	sc := &client.API{}
	sc.Init("sk_test_123", &stripe.Backends{API: backend, Connect: backend, Uploads: backend})

	return &stripeUsageReporter{
		client: sc,
		o11y:   observability.NewObserver(usageImplementationName, loggingnoop.NewLogger(), tracingnoop.NewTracerProvider()),
	}, &captured
}

func TestNewStripeUsageReporter(T *testing.T) {
	T.Parallel()

	T.Run("builds with an API key", func(t *testing.T) {
		t.Parallel()

		reporter, err := NewStripeUsageReporter(&Config{APIKey: "sk_test_123"},
			WithLogger(loggingnoop.NewLogger()), WithTracerProvider(tracingnoop.NewTracerProvider()))
		must.NoError(t, err)
		must.NotNil(t, reporter)
	})

	T.Run("refuses a nil config", func(t *testing.T) {
		t.Parallel()

		_, err := NewStripeUsageReporter(nil)

		test.ErrorIs(t, err, ErrNilConfig)
	})

	T.Run("requires an API key", func(t *testing.T) {
		t.Parallel()

		// Unlike the payment manager there is no inbound path here, so a reporter
		// without a key could do nothing at all and would fail on its first flush
		// rather than at startup.
		_, err := NewStripeUsageReporter(&Config{WebhookSecret: "whsec_123"})

		test.ErrorIs(t, err, ErrAPIKeyNotConfigured)
	})
}

func TestStripeUsageReporter_ReportUsage(T *testing.T) {
	T.Parallel()

	const body = `{"id":"mbur_123","object":"usage_record","quantity":42,"subscription_item":"si_123"}`

	T.Run("posts an increment under the caller's idempotency key", func(t *testing.T) {
		t.Parallel()

		reporter, captured := newTestUsageReporter(t, http.StatusOK, body)

		must.NoError(t, reporter.ReportUsage(t.Context(), &capitalism.UsageReportInput{
			SubscriptionItemID: "si_123",
			Quantity:           42,
			IdempotencyKey:     "mtr_abc",
			Metadata:           map[string]string{"metering_meter": "api_requests"},
		}))

		must.SliceLen(t, 1, *captured)

		req := (*captured)[0]
		test.EqOp(t, http.MethodPost, req.method)
		test.EqOp(t, "/v1/subscription_items/si_123/usage_records", req.path)
		test.EqOp(t, "42", req.form.Get("quantity"))
		// Always increment. The other action, set, overwrites usage at a
		// timestamp — which sounds appealing for a flush that knows the running
		// total, and leaves two flushes for one period standing beside each other
		// rather than replacing one another.
		test.EqOp(t, stripe.UsageRecordActionIncrement, req.form.Get("action"))
		test.EqOp(t, "mtr_abc", req.idempotencyKey)
		// Metadata is dropped rather than forwarded: Stripe's usage record object
		// carries none, and the SDK refuses the request rather than ignoring the
		// field. See the comment in ReportUsage.
		test.EqOp(t, "", req.form.Get("metadata[metering_meter]"))
	})

	T.Run("stamps now for a zero event time", func(t *testing.T) {
		t.Parallel()

		reporter, captured := newTestUsageReporter(t, http.StatusOK, body)

		must.NoError(t, reporter.ReportUsage(t.Context(), &capitalism.UsageReportInput{
			SubscriptionItemID: "si_123",
			Quantity:           1,
			IdempotencyKey:     "mtr_abc",
		}))

		must.SliceLen(t, 1, *captured)

		// "now" rather than a locally computed timestamp: Stripe rejects a usage
		// record dated in the future, and a worker whose clock runs a few seconds
		// fast would otherwise have every flush refused.
		test.EqOp(t, "now", (*captured)[0].form.Get("timestamp"))
	})

	T.Run("stamps an explicit event time", func(t *testing.T) {
		t.Parallel()

		reporter, captured := newTestUsageReporter(t, http.StatusOK, body)

		occurredAt := time.Date(2026, time.August, 31, 23, 59, 59, 0, time.UTC)

		must.NoError(t, reporter.ReportUsage(t.Context(), &capitalism.UsageReportInput{
			SubscriptionItemID: "si_123",
			Quantity:           1,
			IdempotencyKey:     "mtr_abc",
			OccurredAt:         occurredAt,
		}))

		must.SliceLen(t, 1, *captured)
		test.EqOp(t, "1788220799", (*captured)[0].form.Get("timestamp"))
	})

	T.Run("refuses a nil input", func(t *testing.T) {
		t.Parallel()

		reporter, captured := newTestUsageReporter(t, http.StatusOK, body)

		test.ErrorIs(t, reporter.ReportUsage(t.Context(), nil), platformerrors.ErrNilInputParameter)
		test.SliceEmpty(t, *captured)
	})

	T.Run("refuses an empty subscription item", func(t *testing.T) {
		t.Parallel()

		reporter, captured := newTestUsageReporter(t, http.StatusOK, body)

		// Stripe puts the subscription item in the request path, so an empty one
		// would be a request to a different endpoint entirely.
		test.ErrorIs(t, reporter.ReportUsage(t.Context(), &capitalism.UsageReportInput{
			Quantity: 1, IdempotencyKey: "mtr_abc",
		}), ErrEmptySubscriptionItem)

		test.SliceEmpty(t, *captured)
	})

	T.Run("refuses an empty idempotency key", func(t *testing.T) {
		t.Parallel()

		reporter, captured := newTestUsageReporter(t, http.StatusOK, body)

		// Refused rather than defaulted: a post with no key double-bills on
		// retry, and generating one here would make every attempt distinct, which
		// is precisely the wrong answer.
		test.ErrorIs(t, reporter.ReportUsage(t.Context(), &capitalism.UsageReportInput{
			SubscriptionItemID: "si_123", Quantity: 1,
		}), ErrEmptyUsageIdempotencyKey)

		test.SliceEmpty(t, *captured)
	})

	T.Run("propagates a provider error", func(t *testing.T) {
		t.Parallel()

		reporter, _ := newTestUsageReporter(t, http.StatusBadRequest,
			`{"error":{"message":"no such subscription item","type":"invalid_request_error"}}`)

		test.Error(t, reporter.ReportUsage(t.Context(), &capitalism.UsageReportInput{
			SubscriptionItemID: "si_123", Quantity: 1, IdempotencyKey: "mtr_abc",
		}))
	})
}
