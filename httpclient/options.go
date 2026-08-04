package httpclient

import (
	"context"
	"net"
	"net/http"
	"time"
)

// DialContextFunc is the shape of net.Dialer's DialContext.
//
// It is an alias rather than a defined type so that a dialer written against
// the raw signature — or against another package's alias for it — is passed
// here without a conversion.
type DialContextFunc = func(ctx context.Context, network, address string) (net.Conn, error)

// Option customizes the HTTP client returned by NewHTTPClient. Options are
// applied in order, so a later Option overrides an earlier one.
type Option func(*clientConfig)

// clientConfig is the resolved client configuration.
type clientConfig struct {
	// transport, when non-nil, is used as the client's base RoundTripper instead
	// of one built from the settings below. Tracing, if enabled, still wraps it.
	transport           http.RoundTripper
	dialWrapper         func(DialContextFunc) DialContextFunc
	timeout             time.Duration
	maxIdleConns        int
	maxIdleConnsPerHost int
	tracing             bool
}

// newClientConfig builds the default client configuration.
func newClientConfig() *clientConfig {
	return &clientConfig{
		timeout:             defaultTimeout,
		maxIdleConns:        defaultMaxIdleConns,
		maxIdleConnsPerHost: defaultMaxIdleConnsPerHost,
	}
}

// WithTimeout sets the client's overall request timeout, which also bounds the
// dial. A non-positive duration leaves the default (defaultTimeout) in place.
func WithTimeout(timeout time.Duration) Option {
	return func(c *clientConfig) {
		if timeout > 0 {
			c.timeout = timeout
		}
	}
}

// WithMaxIdleConns sets the transport's maximum number of idle connections across
// all hosts. A non-positive value leaves the default in place. It has no effect
// alongside WithTransport.
func WithMaxIdleConns(n int) Option {
	return func(c *clientConfig) {
		if n > 0 {
			c.maxIdleConns = n
		}
	}
}

// WithMaxIdleConnsPerHost sets the transport's maximum number of idle connections
// per host. A non-positive value leaves the default in place. It has no effect
// alongside WithTransport.
func WithMaxIdleConnsPerHost(n int) Option {
	return func(c *clientConfig) {
		if n > 0 {
			c.maxIdleConnsPerHost = n
		}
	}
}

// WithTracing toggles wrapping the transport in OpenTelemetry instrumentation.
// Tracing is off by default.
func WithTracing(enabled bool) Option {
	return func(c *clientConfig) { c.tracing = enabled }
}

// WithDialWrapper wraps the dial function of the transport this package builds.
//
// It wraps rather than replaces because the dialer carries settings a caller
// should not have to restate to interpose on it — the timeout WithTimeout set
// and the keep-alive interval — and a replacement that forgot either would look
// like it worked. The wrapper receives the dialer those settings produced and
// returns the one the transport will use.
//
// This is the seam for a dial that has to decide where it is allowed to connect:
// webhooks.PinningDialContext, which refuses any address the SSRF check did not
// vet, has exactly this shape. It has no effect alongside WithTransport, which
// supplies a RoundTripper whose dialing this package does not own. A nil wrapper
// is ignored.
func WithDialWrapper(wrap func(base DialContextFunc) DialContextFunc) Option {
	return func(c *clientConfig) {
		if wrap != nil {
			c.dialWrapper = wrap
		}
	}
}

// WithTransport uses the given RoundTripper as the client's base transport rather
// than building one, which is the seam for stubbing responses in tests or layering
// custom middleware. The connection-pool options are ignored when it is set;
// tracing, if enabled, still wraps it. A nil RoundTripper is ignored.
func WithTransport(transport http.RoundTripper) Option {
	return func(c *clientConfig) {
		if transport != nil {
			c.transport = transport
		}
	}
}
