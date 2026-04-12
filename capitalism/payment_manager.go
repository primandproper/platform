package capitalism

import (
	"net/http"
)

type (
	// SubscriptionPlan describes a plan you pay on a recurring monthly basis for.
	SubscriptionPlan struct {
		ID    string
		Name  string
		Price uint32
	}

	// PaymentManager handles payments via 3rd-party providers.
	PaymentManager interface {
		HandleEventWebhook(req *http.Request) error
	}
)
