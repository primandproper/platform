package httpclient

import (
	"net/http"
)

// DefaultErrorBodyLimit is how much of a refused response's body a StatusError
// keeps when no other bound is named.
//
// 512 bytes is enough for every error shape a service designs on purpose — a
// JSON problem document, a validation list, a plain sentence — and short enough
// that the shapes nobody designed, an HTML error page from a load balancer or a
// stack trace from a framework's debug mode, cost one log line rather than a
// log budget.
const DefaultErrorBodyLimit = 512

// ExchangeOption customizes a single JSON exchange. Options are applied in
// order, so a later one overrides an earlier one.
//
// It is per call rather than per client because the two things it governs are
// per call: the headers this request needs, and how much of this endpoint's
// error body is worth keeping. Everything durable about a client — its timeout,
// its transports, its observability — is an Option on NewHTTPClient, and
// nothing here is a second way to set any of it.
type ExchangeOption func(*exchangeConfig)

// exchangeConfig is the resolved configuration for one exchange.
type exchangeConfig struct {
	header         http.Header
	errorBodyLimit int
}

// newExchangeConfig resolves the options for one exchange, defaults first.
func newExchangeConfig(opts []ExchangeOption) *exchangeConfig {
	cfg := &exchangeConfig{
		header:         http.Header{},
		errorBodyLimit: DefaultErrorBodyLimit,
	}

	for _, opt := range opts {
		if opt != nil {
			opt(cfg)
		}
	}

	return cfg
}

// WithHeader sets a request header on the exchange, replacing any value the
// exchange would have set itself.
//
// It is the seam for the per-request headers a JSON API asks for and a
// transport cannot know about: an Idempotency-Key, a tenant, a request
// identifier, a vendor media type in place of the Accept and Content-Type this
// package fills in. Credentials that hold for every call belong further down —
// in a transport wrapped by WithTransport, or in WithRequestSigning — rather
// than repeated at every call site, where the one that gets forgotten is the
// one that matters.
//
// An empty name is ignored.
func WithHeader(name, value string) ExchangeOption {
	return func(c *exchangeConfig) {
		if name != "" {
			c.header.Set(name, value)
		}
	}
}

// WithErrorBodyLimit bounds how many bytes of a refused response's body a
// StatusError keeps, and how many are read from the wire at all.
//
// A negative limit leaves DefaultErrorBodyLimit in place. Zero is a real
// answer, and the one to give an endpoint whose failures are known to be
// worthless or sensitive: the body is neither read nor kept, and the error
// reports the status alone.
func WithErrorBodyLimit(bytes int) ExchangeOption {
	return func(c *exchangeConfig) {
		if bytes >= 0 {
			c.errorBodyLimit = bytes
		}
	}
}
