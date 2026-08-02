package emailcfg

import (
	"context"
	"net/http"
	"slices"
	"strings"

	"github.com/primandproper/platform-go/v9/circuitbreaking"
	circuitbreakingcfg "github.com/primandproper/platform-go/v9/circuitbreaking/config"
	"github.com/primandproper/platform-go/v9/email"
	"github.com/primandproper/platform-go/v9/email/mailgun"
	"github.com/primandproper/platform-go/v9/email/mailjet"
	"github.com/primandproper/platform-go/v9/email/noop"
	"github.com/primandproper/platform-go/v9/email/postmark"
	"github.com/primandproper/platform-go/v9/email/resend"
	"github.com/primandproper/platform-go/v9/email/sendgrid"
	"github.com/primandproper/platform-go/v9/email/ses"
	"github.com/primandproper/platform-go/v9/errors"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

const (
	// ProviderSendgrid represents SendGrid.
	ProviderSendgrid = "sendgrid"
	// ProviderMailgun represents Mailgun.
	ProviderMailgun = "mailgun"
	// ProviderMailjet represents Mailjet.
	ProviderMailjet = "mailjet"
	// ProviderResend represents Resend.
	ProviderResend = "resend"
	// ProviderPostmark represents Postmark.
	ProviderPostmark = "postmark"
	// ProviderSES represents AWS SES.
	ProviderSES = "ses"
	// ProviderNoop discards every message. It must be selected deliberately —
	// an unset or typo'd provider is an error, because outbound mail that
	// silently goes nowhere is discovered by the people who never received it.
	ProviderNoop = "noop"
)

// providers are every provider this package implements. The dispatch switch and
// ValidateWithContext both read it, so they cannot drift apart.
var providers = []string{
	ProviderNoop,
	ProviderSendgrid,
	ProviderMailgun,
	ProviderMailjet,
	ProviderResend,
	ProviderPostmark,
	ProviderSES,
}

// knownProvider reports whether p names an implementation, ignoring case and
// surrounding space, exactly as the dispatch switch does.
func knownProvider(p string) bool {
	return slices.Contains(providers, strings.ToLower(strings.TrimSpace(p)))
}

type (
	// Config is the configuration structure.
	Config struct {
		Sendgrid       *sendgrid.Config          `env:",init"    envPrefix:"SENDGRID_"         json:"sendgrid"             yaml:"sendgrid"`
		Mailgun        *mailgun.Config           `env:",init"    envPrefix:"MAILGUN_"          json:"mailgun"              yaml:"mailgun"`
		Mailjet        *mailjet.Config           `env:",init"    envPrefix:"MAILJET_"          json:"mailjet"              yaml:"mailjet"`
		Resend         *resend.Config            `env:",init"    envPrefix:"RESEND_"           json:"resend"               yaml:"resend"`
		Postmark       *postmark.Config          `env:",init"    envPrefix:"POSTMARK_"         json:"postmark"             yaml:"postmark"`
		SES            *ses.Config               `env:",init"    envPrefix:"SES_"              json:"ses"                  yaml:"ses"`
		Provider       string                    `env:"PROVIDER" json:"provider"               yaml:"provider"`
		CircuitBreaker circuitbreakingcfg.Config `env:",init"    envPrefix:"CIRCUIT_BREAKING_" json:"circuitBreakerConfig" yaml:"circuitBreakerConfig"`
	}
)

var _ validation.ValidatableWithContext = (*Config)(nil)

// EnsureDefaults sets sensible defaults for zero-valued fields.
func (cfg *Config) EnsureDefaults() {
	cfg.CircuitBreaker.EnsureDefaults()
}

// ValidateWithContext validates a Config.
func (cfg *Config) ValidateWithContext(ctx context.Context) error {
	return validation.ValidateStructWithContext(
		ctx,
		cfg,
		validation.Field(&cfg.Provider, validation.Required, validation.By(func(any) error {
			if !knownProvider(cfg.Provider) {
				return errors.Wrapf(errors.ErrUnknownProvider, "email provider %q", cfg.Provider)
			}

			return nil
		})),
		validation.Field(&cfg.Sendgrid, validation.When(cfg.Provider == ProviderSendgrid, validation.Required)),
		validation.Field(&cfg.Mailgun, validation.When(cfg.Provider == ProviderMailgun, validation.Required)),
		validation.Field(&cfg.Mailjet, validation.When(cfg.Provider == ProviderMailjet, validation.Required)),
		validation.Field(&cfg.Resend, validation.When(cfg.Provider == ProviderResend, validation.Required)),
		validation.Field(&cfg.Postmark, validation.When(cfg.Provider == ProviderPostmark, validation.Required)),
		validation.Field(&cfg.SES, validation.When(cfg.Provider == ProviderSES, validation.Required)),
	)
}

// NewEmailer provides an outbound_emailer.
func (cfg *Config) NewEmailer(ctx context.Context, client *http.Client, circuitBreaker circuitbreaking.CircuitBreaker, opts ...Option) (email.Emailer, error) {
	o := newOptions(opts)
	logger, tracerProvider, metricsProvider := o.logger, o.tracerProvider, o.metricsProvider

	cfg.EnsureDefaults()

	// The provider is checked before the rest of the config so an unrecognized
	// one reports ErrUnknownProvider rather than whichever sub-config happened
	// to be missing as a consequence.
	if !knownProvider(cfg.Provider) {
		return nil, errors.Wrapf(errors.ErrUnknownProvider, "email provider %q", cfg.Provider)
	}

	if err := cfg.ValidateWithContext(ctx); err != nil {
		return nil, errors.Wrap(err, "validating email config")
	}

	switch strings.ToLower(strings.TrimSpace(cfg.Provider)) {
	case ProviderSendgrid:
		return sendgrid.NewSendGridEmailer(cfg.Sendgrid, client, circuitBreaker, sendgrid.WithLogger(logger), sendgrid.WithTracerProvider(tracerProvider), sendgrid.WithMetricsProvider(metricsProvider))
	case ProviderMailgun:
		return mailgun.NewMailgunEmailer(cfg.Mailgun, client, circuitBreaker, mailgun.WithLogger(logger), mailgun.WithTracerProvider(tracerProvider), mailgun.WithMetricsProvider(metricsProvider))
	case ProviderMailjet:
		return mailjet.NewMailjetEmailer(cfg.Mailjet, client, circuitBreaker, mailjet.WithLogger(logger), mailjet.WithTracerProvider(tracerProvider), mailjet.WithMetricsProvider(metricsProvider))
	case ProviderResend:
		return resend.NewResendEmailer(cfg.Resend, client, circuitBreaker, resend.WithLogger(logger), resend.WithTracerProvider(tracerProvider), resend.WithMetricsProvider(metricsProvider))
	case ProviderPostmark:
		return postmark.NewPostmarkEmailer(cfg.Postmark, client, circuitBreaker, postmark.WithLogger(logger), postmark.WithTracerProvider(tracerProvider), postmark.WithMetricsProvider(metricsProvider))
	case ProviderSES:
		return ses.NewSESEmailer(ctx, cfg.SES, client, circuitBreaker, nil, ses.WithLogger(logger), ses.WithTracerProvider(tracerProvider), ses.WithMetricsProvider(metricsProvider))
	case ProviderNoop:
		return noop.NewEmailer()
	default:
		return nil, errors.Wrapf(errors.ErrUnknownProvider, "email provider %q", cfg.Provider)
	}
}
