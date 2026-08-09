package gcp

import (
	"context"

	"github.com/primandproper/platform-go/v10/observability"
	"github.com/primandproper/platform-go/v10/secrets"

	"github.com/samber/do/v2"
)

// RegisterSecretSource registers this implementation under two keys: its own
// type, *SecretSource, and secrets.SecretSource. Both resolve to the same
// source.
//
// The concrete key is the point. A caller who has chosen GCP Secret Manager can
// depend on the thing they chose rather than on the interface every provider
// shares, and so can reason about what this one alone does: reach one project
// over the network, once per lookup. The interface key is an alias rather than
// a second provider, so both callers share one source and one API client.
//
// Prerequisites: context.Context and *Config must be registered in the injector
// before the source is invoked.
//
// The API client and the options are passed here rather than invoked. The
// client because a nil one means "build one from Application Default
// Credentials" — which is what a container that registered none wants, and is
// not something a failed do.Invoke can say. The options because this package's
// remaining knobs live in secretscfg, which imports this package and so cannot
// be imported back.
//
// Observability comes from the injector's pillars when it has any. A container
// that registers none still wires up — every pillar resolves to its noop — but
// one whose registered provider fails to build reports that rather than
// degrading to a source that looks instrumented and records nowhere.
func RegisterSecretSource(i do.Injector, client SecretVersionAccessor, opts ...Option) {
	do.Provide(i, func(i do.Injector) (*SecretSource, error) {
		pillars, err := observability.InvokePillars(i)
		if err != nil {
			return nil, err
		}

		// The caller's options are applied after the pillars, so an explicit
		// WithMetricsProvider(nil) leaves that one component unmetered even in
		// a container that registered a provider.
		return NewSecretSource(
			do.MustInvoke[context.Context](i),
			do.MustInvoke[*Config](i),
			client,
			append([]Option{
				WithLogger(pillars.Logger),
				WithTracerProvider(pillars.TracerProvider),
				WithMetricsProvider(pillars.MetricsProvider),
			}, opts...)...,
		)
	})

	// Cannot fail: *SecretSource implements secrets.SecretSource — the compiler
	// says so at the top of gcp.go — and the service it aliases was just
	// provided.
	do.MustAs[*SecretSource, secrets.SecretSource](i)
}
