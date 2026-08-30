package noop

import (
	"net/http"
	"testing"

	"github.com/primandproper/platform-go/v13/capitalism"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestPaymentManager_HandleEventWebhook(T *testing.T) {
	T.Parallel()

	T.Run("accepts the delivery and reports no event", func(t *testing.T) {
		t.Parallel()
		mgr := NewPaymentManager()
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://example.com/webhook", http.NoBody)
		must.NoError(t, err)

		event, err := mgr.HandleEventWebhook(req)
		test.NoError(t, err)
		// Nil rather than a zero Event: no provider sent this, so there is no standing to
		// report, and a zero value would report an unknown one.
		test.Nil(t, event)
	})
}

func TestPaymentManager_ImplementsInterface(T *testing.T) {
	T.Parallel()

	T.Run("satisfies PaymentManager", func(t *testing.T) {
		t.Parallel()
		_ = NewPaymentManager()
	})
}

func TestPaymentManager_ProviderSideWrites(T *testing.T) {
	T.Parallel()

	// The three operations that would create state at a provider all report
	// ErrPaymentsDisabled rather than the empty IDs and nil errors they once
	// returned. An empty customer ID stored as though it were real is a bug that
	// surfaces months later, against a provider that has never heard of the
	// account — so this manager is deliberately loud where its webhook handler
	// is quiet.
	T.Run("CreateCustomer reports payments are disabled", func(t *testing.T) {
		t.Parallel()

		mgr := NewPaymentManager()

		customerID, err := mgr.CreateCustomer(t.Context(), &capitalism.CustomerCreationInput{Name: "Acme"})
		test.ErrorIs(t, err, capitalism.ErrPaymentsDisabled)
		test.EqOp(t, "", customerID)
	})

	T.Run("CreatePaymentIntent reports payments are disabled", func(t *testing.T) {
		t.Parallel()

		mgr := NewPaymentManager()

		intent, err := mgr.CreatePaymentIntent(t.Context(), &capitalism.PaymentIntentCreationInput{})
		test.ErrorIs(t, err, capitalism.ErrPaymentsDisabled)

		// Nil rather than a zero intent, which would describe a charge that
		// never happened.
		test.Nil(t, intent)
	})

	T.Run("CreateSubscription reports payments are disabled", func(t *testing.T) {
		t.Parallel()

		mgr := NewPaymentManager()

		subscriptionID, err := mgr.CreateSubscription(t.Context(), &capitalism.SubscriptionCreationInput{})
		test.ErrorIs(t, err, capitalism.ErrPaymentsDisabled)
		test.EqOp(t, "", subscriptionID)
	})

	T.Run("a nil input is refused the same way", func(t *testing.T) {
		t.Parallel()

		// There is no provider to validate against, so the answer does not
		// depend on what was asked.
		mgr := NewPaymentManager()

		_, err := mgr.CreateCustomer(t.Context(), nil)
		test.ErrorIs(t, err, capitalism.ErrPaymentsDisabled)

		_, err = mgr.CreatePaymentIntent(t.Context(), nil)
		test.ErrorIs(t, err, capitalism.ErrPaymentsDisabled)

		_, err = mgr.CreateSubscription(t.Context(), nil)
		test.ErrorIs(t, err, capitalism.ErrPaymentsDisabled)
	})
}

func TestUsageReporter_ReportUsage(T *testing.T) {
	T.Parallel()

	T.Run("returns nil", func(t *testing.T) {
		t.Parallel()

		// What a deployment that meters but does not bill runs: usage still
		// accumulates durably and quotas are still enforced, and nothing reaches a
		// provider. A normal configuration, not a degraded one.
		reporter := NewUsageReporter()

		test.NoError(t, reporter.ReportUsage(t.Context(), &capitalism.UsageReportInput{
			CustomerID: "cus_123", MeterName: "api_requests", Quantity: 1, IdempotencyKey: "mtr_abc",
		}))
		test.NoError(t, reporter.ReportUsage(t.Context(), nil))
	})
}
