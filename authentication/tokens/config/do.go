package tokenscfg

import (
	"github.com/primandproper/platform-go/v9/authentication/tokens"
	"github.com/primandproper/platform-go/v9/observability"

	"github.com/samber/do/v2"
)

// NewTokenIssuer provides a tokens.Issuer from a config.
func NewTokenIssuer(cfg *Config, opts ...Option) (tokens.Issuer, error) {
	return cfg.NewTokenIssuer(opts...)
}

// RegisterTokenIssuer registers the token issuer with the injector.
func RegisterTokenIssuer(i do.Injector) {
	do.Provide[tokens.Issuer](i, func(i do.Injector) (tokens.Issuer, error) {
		pillars, err := observability.InvokePillars(i)
		if err != nil {
			return nil, err
		}

		return NewTokenIssuer(
			do.MustInvoke[*Config](i),
			WithPillars(pillars),
		)
	})
}
