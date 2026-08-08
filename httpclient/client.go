package httpclient

import (
	"net"
	"net/http"
	"time"

	platformerrors "github.com/primandproper/platform-go/v10/errors"
	"github.com/primandproper/platform-go/v10/observability/tracing"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

const (
	defaultKeepAlive           = 30 * time.Second
	defaultTLSHandshakeTimeout = 10 * time.Second
)

// buildClient constructs an HTTP client from resolved options.
func (c *clientConfig) buildClient() (*http.Client, error) {
	obs, err := newTransportObserver(c.logger, c.tracerProvider, c.metricsProvider)
	if err != nil {
		return nil, platformerrors.Wrap(err, "instrumenting the HTTP client")
	}

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
		// The provider is passed explicitly: otelhttp falls back to the
		// OpenTelemetry global when it is not, which means a service that
		// configured a provider of its own would have its client spans quietly
		// go somewhere else. EnsureTracerProvider makes the absent case a noop
		// provider rather than that global.
		transport = otelhttp.NewTransport(
			transport,
			otelhttp.WithSpanNameFormatter(tracing.FormatSpan),
			otelhttp.WithTracerProvider(tracing.EnsureTracerProvider(c.tracerProvider)),
		)
	}

	// The resilience layers wrap inside-out, so this reads bottom-up: the rate
	// limiter is nearest the wire and the breaker is outermost. The nesting is
	// fixed rather than following the order the options were passed, because
	// only one arrangement of the three is correct and a caller who got it wrong
	// would have a client that looks protected and is not.
	//
	// Tracing sits below all of them, so each attempt is its own client span
	// rather than one span covering a loop.
	resilient := false

	if c.rateLimiter != nil {
		c.rateLimiter.base = transport
		c.rateLimiter.obs = obs
		transport = c.rateLimiter
		resilient = true
	}

	// Retry above the limiter: every attempt it makes spends a token, so a retry
	// storm is charged against the provider's budget instead of slipping past it.
	if c.retry != nil {
		c.retry.base = transport
		c.retry.obs = obs
		transport = c.retry
		resilient = true
	}

	// The breaker outermost: an open circuit rejects before the retry loop is
	// entered, which is the difference between failing fast and failing fast
	// three times with backoff in between.
	if c.breaker != nil {
		c.breaker.base = transport
		c.breaker.obs = obs
		transport = c.breaker
		resilient = true
	}

	// Above even the breaker, and only when there is something for it to
	// describe: a client with no resilience layers has nothing between the
	// caller and the tracing transport, so a second span would only duplicate
	// the one otelhttp already emits.
	if resilient {
		transport = &observedTransport{base: transport, obs: obs}
	}

	return &http.Client{
		Transport: transport,
		Timeout:   c.timeout,
	}, nil
}
