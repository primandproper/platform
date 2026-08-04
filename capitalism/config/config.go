package capitalismcfg

import (
	"context"
	"strings"

	"github.com/primandproper/platform-go/v9/capitalism"
	"github.com/primandproper/platform-go/v9/capitalism/noop"
	"github.com/primandproper/platform-go/v9/capitalism/stripe"
	"github.com/primandproper/platform-go/v9/errors"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

const (
	// StripeProvider is the key that indicates Stripe should be used for payments.
	StripeProvider = "stripe"
	// NoopProvider charges nothing and reports no usage. It must be selected
	// deliberately — an unset or typo'd provider is an error, because a payment
	// manager that silently accepts every call without charging anyone looks like
	// a working deployment right up until someone reconciles the books.
	//
	// It is also what makes "meter everything, bill nothing" a supported
	// deployment: metering keeps counting durably and enforcing quotas, and
	// nothing reaches a provider.
	NoopProvider = "noop"
)

type (
	// Config allows for the configuration of this package and its subpackages.
	Config struct {
		Stripe   *stripe.Config `env:",init"    envPrefix:"STRIPE_"       json:"stripe,omitempty"   yaml:"stripe,omitempty"`
		Provider string         `env:"PROVIDER" json:"provider,omitempty" yaml:"provider,omitempty"`
	}
)

var _ validation.ValidatableWithContext = (*Config)(nil)

// ValidateWithContext validates a Config struct.
//
// The Stripe sub-config is skipped rather than merely unguarded when Stripe is
// not the provider: ozzo validates any non-nil pointer to a Validatable once a
// field's rules have run, and `env:",init"` leaves every sub-config non-nil. A
// validation.When guard alone stops the Required rule and nothing else, so a
// webhook secret was demanded of deployments that charge nobody.
func (cfg *Config) ValidateWithContext(ctx context.Context) error {
	return validation.ValidateStructWithContext(ctx, cfg,
		validation.Field(&cfg.Provider, validation.Required, validation.In(StripeProvider, NoopProvider)),
		validation.Field(&cfg.Stripe, validation.Skip.When(cfg.Provider != StripeProvider), validation.Required),
	)
}

// NewPaymentManager provides a capitalism.PaymentManager implementation based on the
// config. stripeEventHandler is optional (may be nil) and, for the Stripe provider, is invoked with
// each verified webhook event.
func NewPaymentManager(_ context.Context, cfg *Config, stripeEventHandler stripe.EventHandler, opts ...Option) (capitalism.PaymentManager, error) {
	o := newOptions(opts)
	logger, tracerProvider := o.logger, o.tracerProvider

	switch strings.TrimSpace(strings.ToLower(cfg.Provider)) {
	case StripeProvider:
		return stripe.NewPaymentManager(cfg.Stripe, stripeEventHandler, stripe.WithLogger(logger), stripe.WithTracerProvider(tracerProvider))
	case NoopProvider:
		return noop.NewPaymentManager(), nil
	default:
		return nil, errors.Wrapf(errors.ErrUnknownProvider, "payments provider %q", cfg.Provider)
	}
}

// NewUsageReporter provides a capitalism.UsageReporter based on the config.
func NewUsageReporter(_ context.Context, cfg *Config, opts ...Option) (capitalism.UsageReporter, error) {
	o := newOptions(opts)
	logger, tracerProvider := o.logger, o.tracerProvider

	switch strings.TrimSpace(strings.ToLower(cfg.Provider)) {
	case StripeProvider:
		return stripe.NewUsageReporter(cfg.Stripe, stripe.WithLogger(logger), stripe.WithTracerProvider(tracerProvider))
	case NoopProvider:
		return noop.NewUsageReporter(), nil
	default:
		return nil, errors.Wrapf(errors.ErrUnknownProvider, "payments provider %q", cfg.Provider)
	}
}
