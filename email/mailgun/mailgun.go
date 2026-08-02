package mailgun

import (
	"context"
	"fmt"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/primandproper/platform-go/v9/circuitbreaking"
	"github.com/primandproper/platform-go/v9/email"
	platformerrors "github.com/primandproper/platform-go/v9/errors"
	"github.com/primandproper/platform-go/v9/observability"
	"github.com/primandproper/platform-go/v9/observability/keys"
	"github.com/primandproper/platform-go/v9/observability/metrics"

	"github.com/mailgun/mailgun-go/v4"
)

const (
	name = "mailgun_emailer"
)

var (
	_ email.Emailer = (*Emailer)(nil)

	// ErrNilConfig indicates a nil config was provided.
	ErrNilConfig = platformerrors.New("mailgun config is nil")
	// ErrEmptyDomain indicates an empty domain was provided.
	ErrEmptyDomain = platformerrors.New("empty domain")
	// ErrEmptyPrivateAPIKey indicates an empty API token was provided.
	ErrEmptyPrivateAPIKey = platformerrors.New("empty Mailgun API token")
	// ErrNilHTTPClient indicates a nil HTTP client was provided.
	ErrNilHTTPClient = platformerrors.New("nil HTTP client")
)

type (
	// Emailer uses Mailgun to send email.
	Emailer struct {
		o11y           observability.Observer
		sendCounter    metrics.Int64Counter
		errorCounter   metrics.Int64Counter
		latencyHist    metrics.Float64Histogram
		client         mailgun.Mailgun
		circuitBreaker circuitbreaking.CircuitBreaker
	}
)

// NewMailgunEmailer returns a new Mailgun-backed Emailer.
func NewMailgunEmailer(cfg *Config, client *http.Client, circuitBreaker circuitbreaking.CircuitBreaker, opts ...Option) (*Emailer, error) {
	if cfg == nil {
		return nil, ErrNilConfig
	}

	if cfg.Domain == "" {
		return nil, ErrEmptyDomain
	}

	if cfg.PrivateAPIKey == "" {
		return nil, ErrEmptyPrivateAPIKey
	}

	if client == nil {
		return nil, ErrNilHTTPClient
	}

	o := newOptions(opts)

	mp := metrics.EnsureMetricsProvider(o.metricsProvider)

	sendCounter, err := mp.NewInt64Counter(fmt.Sprintf("%s_sends", name))
	if err != nil {
		return nil, platformerrors.Wrap(err, "creating send counter")
	}

	errorCounter, err := mp.NewInt64Counter(fmt.Sprintf("%s_errors", name))
	if err != nil {
		return nil, platformerrors.Wrap(err, "creating error counter")
	}

	latencyHist, err := mp.NewFloat64Histogram(fmt.Sprintf("%s_latency_ms", name))
	if err != nil {
		return nil, platformerrors.Wrap(err, "creating latency histogram")
	}

	mg := mailgun.NewMailgun(cfg.Domain, cfg.PrivateAPIKey)
	mg.SetClient(client)

	e := &Emailer{
		o11y:           observability.NewObserver(name, o.logger, o.tracerProvider),
		sendCounter:    sendCounter,
		errorCounter:   errorCounter,
		latencyHist:    latencyHist,
		client:         mg,
		circuitBreaker: circuitBreaker,
	}

	return e, nil
}

// formatAddress renders a display name and address as one RFC 5322 mailbox,
// quoting and escaping the name.
//
// Building it with Sprintf instead — as this package used to — lets a comma in
// an attacker-influenced display name inject extra recipients, because mailgun
// accepts a comma-separated list wherever it accepts an address. The other
// providers in this package escape via mail.Address for the same reason.
func formatAddress(name, address string) string {
	if strings.TrimSpace(name) == "" {
		return address
	}

	return (&mail.Address{Name: name, Address: address}).String()
}

// SendEmail sends an email.
func (e *Emailer) SendEmail(ctx context.Context, details *email.OutboundEmailMessage) error {
	ctx, op := e.o11y.Begin(ctx)
	defer op.End()

	startTime := time.Now()
	defer func() {
		e.latencyHist.Record(ctx, float64(time.Since(startTime).Milliseconds()))
	}()

	op.Set(keys.EmailSubjectKey, details.Subject).Set(keys.EmailToAddressKey, details.ToAddress).Set(keys.EmailFromAddressKey, details.FromAddress)

	if e.circuitBreaker.CannotProceed() {
		return circuitbreaking.ErrCircuitBroken
	}

	msg := mailgun.NewMessage(
		formatAddress(details.FromName, details.FromAddress),
		details.Subject,
		"",
		formatAddress(details.ToName, details.ToAddress),
	)
	msg.SetHTML(details.HTMLContent)

	if _, _, err := e.client.Send(ctx, msg); err != nil {
		e.circuitBreaker.Failed()
		e.errorCounter.Add(ctx, 1)
		return op.Error(err, "sending email")
	}

	e.circuitBreaker.Succeeded()
	e.sendCounter.Add(ctx, 1)
	return nil
}
