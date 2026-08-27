package revenuecat

import (
	"context"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

// Config configures our RevenueCat interface.
//
// It carries one credential because this adapter has one direction. Stripe's
// config has two — an API key that calls out and a secret that verifies what
// comes back — and RevenueCat has nothing to call out to; see the package doc.
type Config struct {
	// WebhookSecret is the signing secret shown on the webhook integration in
	// RevenueCat's dashboard. Required.
	//
	// It is json:"-" and yaml:"-" so that a config dump — a debug endpoint, a
	// startup log line, an error rendering the struct — cannot print the one
	// value that makes this endpoint's verification mean anything. That is
	// webhooks/inbound's config's rule rather than capitalism/stripe's, and it
	// applies with more force here: this struct is nothing but the secret, so a
	// dump of it would be nothing but the secret.
	WebhookSecret string `env:"WEBHOOK_SECRET" json:"-" yaml:"-"`
}

var _ validation.ValidatableWithContext = (*Config)(nil)

// ValidateWithContext validates a Config struct.
//
// The secret is required outright, where Stripe's is required only in the
// absence of an API key. There is no outbound half here for a
// credential-less config to still serve, so a RevenueCat config without a
// secret can do nothing at all — and a manager built from one would reject
// every delivery with a signature error, which reads as RevenueCat's fault
// rather than as a missing environment variable.
func (cfg *Config) ValidateWithContext(ctx context.Context) error {
	return validation.ValidateStructWithContext(ctx, cfg,
		validation.Field(&cfg.WebhookSecret, validation.Required),
	)
}
