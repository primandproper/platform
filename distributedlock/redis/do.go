package redis

import (
	"github.com/primandproper/platform-go/v10/circuitbreaking"
	"github.com/primandproper/platform-go/v10/distributedlock"
	"github.com/primandproper/platform-go/v10/observability"

	"github.com/samber/do/v2"
)

// RegisterLocker registers this implementation under two keys: its own type,
// *Locker, and distributedlock.Locker. Both resolve to the same locker.
//
// The concrete key is the point. A caller who has chosen the redis locker can
// depend on the thing they chose rather than on the interface every provider
// shares, and so can reason about what only this provider does — hold leases in
// redis under a key prefix, refresh them against a clock this process does not
// own. The interface key is an alias rather than a second provider, so two
// collaborators share one locker and one connection pool.
//
// Prerequisite: *Config must be registered in the injector before the locker is
// invoked.
//
// The circuit breaker and the options are passed here rather than invoked. The
// circuit breaker for the reason httpclient.RegisterHTTPClient gives for not
// resolving one either: a CircuitBreaker in a container is far more often the
// one guarding the service's own inbound API, and silently repurposing it to
// guard this locker would be a surprise nobody asked for. The options because
// this package's remaining knobs live in distributedlockcfg, which imports this
// package and so cannot be imported back.
//
// Observability comes from the injector's pillars when it has any. A container
// that registers none still wires up — every pillar resolves to its noop — but
// one whose registered provider fails to build reports that rather than
// degrading to a locker that looks instrumented and records nowhere.
func RegisterLocker(i do.Injector, cb circuitbreaking.CircuitBreaker, opts ...Option) {
	do.Provide(i, func(i do.Injector) (*Locker, error) {
		pillars, err := observability.InvokePillars(i)
		if err != nil {
			return nil, err
		}

		// The caller's options are applied after the pillars, so an explicit
		// WithMetricsProvider(nil) leaves that one component unmetered even in
		// a container that registered a provider.
		return NewRedisLocker(do.MustInvoke[*Config](i), cb, append([]Option{
			WithLogger(pillars.Logger),
			WithTracerProvider(pillars.TracerProvider),
			WithMetricsProvider(pillars.MetricsProvider),
		}, opts...)...)
	})

	// Cannot fail: *Locker implements distributedlock.Locker — the compiler
	// says so at the top of redis.go — and the service it aliases was just
	// provided.
	do.MustAs[*Locker, distributedlock.Locker](i)
}
