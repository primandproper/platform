package stripe

import (
	"context"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

type (
	// Config configures our Stripe interface.
	//
	// The two credentials are the two directions, and a deployment needs
	// whichever ones it uses rather than both: APIKey is what calls Stripe —
	// charges, subscriptions, and every meter event NewUsageReporter posts —
	// and WebhookSecret is what verifies what Stripe sends back. Naming neither
	// configures nothing, and is the one combination validation refuses.
	Config struct {
		APIKey        string `env:"API_KEY"        json:"apiKey,omitempty"        yaml:"apiKey,omitempty"`
		WebhookSecret string `env:"WEBHOOK_SECRET" json:"webhookSecret,omitempty" yaml:"webhookSecret,omitempty"`
	}
)

var _ validation.ValidatableWithContext = (*Config)(nil)

// ValidateWithContext validates a Config struct.
//
// Neither credential is required on its own, because NewPaymentManager
// documents that either half works without the other and requiring the webhook
// secret refused every outbound-only deployment — the ones that charge but
// receive nothing. What is required is that one of them be present: a Stripe
// config with neither can do nothing in either direction, which is the case
// this is here to catch.
func (cfg *Config) ValidateWithContext(ctx context.Context) error {
	return validation.ValidateStructWithContext(ctx, cfg,
		validation.Field(&cfg.APIKey, validation.Required.When(cfg.WebhookSecret == "")),
		validation.Field(&cfg.WebhookSecret, validation.Required.When(cfg.APIKey == "")),
	)
}
