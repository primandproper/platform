package httpclient

import (
	"net/http"
)

// NewHTTPClient provides an HTTP client. With no options it returns a client with
// the package defaults; pass Config.Options to drive it from an environment-loaded
// Config.
//
// The error reports that the client could not be instrumented — the metrics
// provider refused an instrument the resilience layers record to. It is
// returned rather than swallowed because a client that silently records nowhere
// is indistinguishable from one that is simply idle, and the difference only
// becomes interesting during the incident where the dashboard is empty.
func NewHTTPClient(opts ...Option) (*http.Client, error) {
	cfg := newClientConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	return cfg.buildClient()
}
