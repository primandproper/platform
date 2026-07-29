package httpclient

import (
	"net/http"

	"github.com/samber/do/v2"
)

// RegisterHTTPClient registers an *http.Client with the injector, built from the
// injector's *Config. Any opts are applied after the Config and so override it.
func RegisterHTTPClient(i do.Injector, opts ...Option) {
	do.Provide[*http.Client](i, func(i do.Injector) (*http.Client, error) {
		cfg := do.MustInvoke[*Config](i)

		return NewHTTPClient(append(cfg.Options(), opts...)...), nil
	})
}
