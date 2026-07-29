package httpclient

import (
	"net/http"
)

// NewHTTPClient provides an HTTP client. With no options it returns a client with
// the package defaults; pass Config.Options to drive it from an environment-loaded
// Config.
func NewHTTPClient(opts ...Option) *http.Client {
	cfg := newClientConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	return cfg.buildClient()
}
