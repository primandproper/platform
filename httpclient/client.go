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

	return &http.Client{
		Transport: transport,
		Timeout:   c.timeout,
	}
}
