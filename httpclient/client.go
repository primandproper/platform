package httpclient

import (
	"net"
	"net/http"
	"time"

	"github.com/primandproper/platform-go/v9/observability/tracing"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

const (
	defaultKeepAlive           = 30 * time.Second
	defaultTLSHandshakeTimeout = 10 * time.Second
)

// buildClient constructs an HTTP client from resolved options.
func (c *clientConfig) buildClient() *http.Client {
	transport := c.transport
	if transport == nil {
		transport = &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   c.timeout,
				KeepAlive: defaultKeepAlive,
			}).DialContext,
			MaxIdleConns:          c.maxIdleConns,
			MaxIdleConnsPerHost:   c.maxIdleConnsPerHost,
			TLSHandshakeTimeout:   defaultTLSHandshakeTimeout,
			ExpectContinueTimeout: 2 * c.timeout,
			IdleConnTimeout:       3 * c.timeout,
		}
	}

	if c.tracing {
		transport = otelhttp.NewTransport(transport, otelhttp.WithSpanNameFormatter(tracing.FormatSpan))
	}

	// The resilience layers wrap inside-out, so this reads bottom-up: the rate
	// limiter is nearest the wire and the breaker is outermost. The nesting is
	// fixed rather than following the order the options were passed, because
	// only one arrangement of the three is correct and a caller who got it wrong
	// would have a client that looks protected and is not.
	//
	// Tracing sits below all of them, so each attempt is its own client span
	// rather than one span covering a loop.
	if c.rateLimiter != nil {
		c.rateLimiter.base = transport
		transport = c.rateLimiter
	}

	// Retry above the limiter: every attempt it makes spends a token, so a retry
	// storm is charged against the provider's budget instead of slipping past it.
	if c.retry != nil {
		c.retry.base = transport
		transport = c.retry
	}

	// The breaker outermost: an open circuit rejects before the retry loop is
	// entered, which is the difference between failing fast and failing fast
	// three times with backoff in between.
	if c.breaker != nil {
		c.breaker.base = transport
		transport = c.breaker
	}

	return &http.Client{
		Transport: transport,
		Timeout:   c.timeout,
	}
}
