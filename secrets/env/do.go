package env

import (
	"github.com/primandproper/platform-go/v10/observability"
	"github.com/primandproper/platform-go/v10/secrets"

	"github.com/samber/do/v2"
)

// RegisterSecretSource registers this implementation under two keys: its own
// type, *SecretSource, and secrets.SecretSource. Both resolve to the same
// source.
//
// The concrete key is the point. A caller who has chosen the environment source
// can depend on the thing they chose rather than on the interface every
// provider shares, and so need not carry the handling that interface forces on
// them: nothing here reaches a network, so no lookup fails for being
// unreachable, and no lookup is worth caching. The interface key is an alias
// rather than a second provider, so both callers share one source.
//
// Nothing is invoked: this source reads the process environment, which needs no
// config. The options are passed here rather than invoked because this
// package's remaining knobs live in secretscfg, which imports this package and
// so cannot be imported back.
//
// Observability comes from the injector's pillars when it has any. A container
// that registers none still wires up — every pillar resolves to its noop — but
// one whose registered provider fails to build reports that rather than
// degrading to a source that looks instrumented and records nowhere.
func RegisterSecretSource(i do.Injector, opts ...Option) {
	do.Provide(i, func(i do.Injector) (*SecretSource, error) {
		pillars, err := observability.InvokePillars(i)
		if err != nil {
			return nil, err
		}

		// The caller's options are applied after the pillars, so an explicit
		// WithMetricsProvider(nil) leaves that one component unmetered even in
		// a container that registered a provider.
		return NewSecretSource(append([]Option{
			WithLogger(pillars.Logger),
			WithTracerProvider(pillars.TracerProvider),
			WithMetricsProvider(pillars.MetricsProvider),
		}, opts...)...)
	})

	// Cannot fail: *SecretSource implements secrets.SecretSource — the compiler
	// says so at the top of env.go — and the service it aliases was just
	// provided.
	do.MustAs[*SecretSource, secrets.SecretSource](i)
}
