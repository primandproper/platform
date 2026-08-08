package httpclient

import (
	"net/http"
	"time"

	"github.com/primandproper/platform-go/v9/circuitbreaking"
	"github.com/primandproper/platform-go/v9/circuitbreaking/partitioned"
	"github.com/primandproper/platform-go/v9/ratelimiting"
	"github.com/primandproper/platform-go/v9/retry"
)

// Option customizes the HTTP client returned by NewHTTPClient. Options are
// applied in order, so a later Option overrides an earlier one.
type Option func(*clientConfig)

// clientConfig is the resolved client configuration.
type clientConfig struct {
	// transport, when non-nil, is used as the client's base RoundTripper instead
	// of one built from the settings below. Tracing, if enabled, still wraps it.
	transport http.RoundTripper

	// The resilience middlewares, each held unattached until buildClient knows
	// what it is wrapping. Nil means the client does not have that layer at all,
	// rather than having one that does nothing.
	breaker     *breakerTransport
	retry       *retryTransport
	rateLimiter *rateLimitTransport

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

// WithRetryPolicy retries failed requests through policy.
//
// Only idempotent methods are retried, and only when the request body can be
// replayed — see WithRetryMethods for both caveats. A response is retried when
// it is a 5xx, a 408, or a 429; every other 4xx is reported to policy as
// retry.Unretryable, so the loop stops on the first one instead of spending its
// attempts re-asking a question already answered. Retry-After is honored up to
// DefaultMaxRetryAfter.
//
// When the attempts run out the caller gets the last response, not an error: a
// 503 that survived three tries is still the server's answer, and code reading
// the status does not have to learn a second way to find it.
//
// The client's overall timeout bounds the whole loop, retries included, because
// http.Client applies it to the request context before the transport ever runs.
// A client that retries wants that budget raised to match. A nil policy is
// ignored.
func WithRetryPolicy(policy retry.Policy, opts ...RetryOption) Option {
	return func(c *clientConfig) {
		if policy != nil {
			c.retry = newRetryTransport(policy, opts)
		}
	}
}

// WithCircuitBreaker fails requests fast once breaker has tripped, so one dead
// dependency stops tying up connections and timeouts.
//
// The breaker sees a request's final outcome, after any retrying: transport
// errors and 5xx responses count as failures, everything else as a success. One
// breaker is shared across every host the client talks to, which is the right
// shape when the client belongs to a single integration. Use
// WithKeyedCircuitBreaker for a client that fans out. A nil breaker is ignored.
func WithCircuitBreaker(breaker circuitbreaking.CircuitBreaker) Option {
	return func(c *clientConfig) {
		if breaker != nil {
			c.breaker = &breakerTransport{breakers: partitioned.NewKeyedCircuitBreaker(breaker, nil)}
		}
	}
}

// WithKeyedCircuitBreaker breaks per host rather than per client, keyed by the
// request URL's host and port.
//
// Hosts registered with the KeyedCircuitBreaker get their own breaker; the rest
// share its global one, so a client that talks to one critical dependency and
// several incidental ones can isolate the dependency without enumerating the
// world. A nil KeyedCircuitBreaker is ignored, as is a key that resolves to no
// breaker at all.
func WithKeyedCircuitBreaker(breakers partitioned.KeyedCircuitBreaker) Option {
	return func(c *clientConfig) {
		if breakers != nil {
			c.breaker = &breakerTransport{breakers: breakers}
		}
	}
}

// WithRateLimit spends a token from limiter, keyed by the request URL's host and
// port, before each request reaches the wire.
//
// It is the layer closest to the network, so every attempt a retry loop makes
// pays for itself — which is the point, since a provider's documented budget
// counts requests, not the caller's intentions. A refused request fails with
// ratelimiting.ErrRateLimited; when a retry policy is also installed, its
// backoff is what waits for the bucket to refill. A nil limiter is ignored.
func WithRateLimit(limiter ratelimiting.RateLimiter) Option {
	return func(c *clientConfig) {
		if limiter != nil {
			c.rateLimiter = &rateLimitTransport{limiter: limiter}
		}
	}
}
