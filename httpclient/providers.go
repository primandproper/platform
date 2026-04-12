package httpclient

import (
	"net/http"
)

// ProvideHTTPClient provides an HTTP client from config.
// If cfg is nil, defaults are used.
func ProvideHTTPClient(cfg *Config) *http.Client {
	if cfg == nil {
		cfg = &Config{}
	}
	cfg.EnsureDefaults()
	return cfg.BuildClient()
}
