package noop

import (
	"context"
	"net/http"

	"github.com/primandproper/platform-go/v9/capitalism"
)

var _ capitalism.PaymentManager = (*paymentManager)(nil)

// paymentManager is a no-op payment manager.
type paymentManager struct{}

// HandleEventWebhook satisfies our interface.
func (n *paymentManager) HandleEventWebhook(_ *http.Request) error {
	return nil
}

// CreateCustomer satisfies our interface.
func (n *paymentManager) CreateCustomer(_ context.Context, _ *capitalism.CustomerCreationInput) (string, error) {
	return "", nil
}

// CreatePaymentIntent satisfies our interface.
func (n *paymentManager) CreatePaymentIntent(_ context.Context, _ *capitalism.PaymentIntentCreationInput) (*capitalism.PaymentIntent, error) {
	return &capitalism.PaymentIntent{}, nil
}

// CreateSubscription satisfies our interface.
func (n *paymentManager) CreateSubscription(_ context.Context, _ *capitalism.SubscriptionCreationInput) (string, error) {
	return "", nil
}

// NewPaymentManager returns a no-op PaymentManager.
func NewPaymentManager() capitalism.PaymentManager {
	return &paymentManager{}
}

var _ capitalism.UsageReporter = (*usageReporter)(nil)

// usageReporter is a no-op usage reporter.
type usageReporter struct{}

// ReportUsage satisfies our interface.
func (n *usageReporter) ReportUsage(_ context.Context, _ *capitalism.UsageReportInput) error {
	return nil
}

// NewUsageReporter returns a no-op UsageReporter.
//
// It is what a deployment that meters but does not bill runs: usage still
// accumulates durably and quotas are still enforced, and nothing is posted to a
// provider. That is a normal configuration — an internal quota system — rather
// than a degraded one.
func NewUsageReporter() capitalism.UsageReporter {
	return &usageReporter{}
}
