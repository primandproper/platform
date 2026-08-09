package postgres

import (
	"github.com/primandproper/platform-go/v10/circuitbreaking"
	"github.com/primandproper/platform-go/v10/database"
	"github.com/primandproper/platform-go/v10/distributedlock"
	"github.com/primandproper/platform-go/v10/observability"

	"github.com/samber/do/v2"
)

// RegisterLocker registers this implementation under two keys: its own type,
// *Locker, and distributedlock.Locker. Both resolve to the same locker.
//
// The concrete key is the point. A caller who has chosen the postgres locker
// can depend on the thing they chose rather than on the interface every
// provider shares, and so can reason about what this provider alone does: hold
// each lock on a pinned write-pool connection, in the same database the rest of
// their work is already in. The interface key is an alias rather than a second
// provider, so two collaborators share one locker rather than each pinning
// their own connections.
//
// Prerequisites: *Config and database.Client must be registered in the injector
// before the locker is invoked.
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
		return NewPostgresLocker(
			do.MustInvoke[*Config](i),
			do.MustInvoke[database.Client](i),
			cb,
			append([]Option{
				WithLogger(pillars.Logger),
				WithTracerProvider(pillars.TracerProvider),
				WithMetricsProvider(pillars.MetricsProvider),
			}, opts...)...,
		)
	})

	// Cannot fail: *Locker implements distributedlock.Locker — the compiler
	// says so at the top of postgres.go — and the service it aliases was just
	// provided.
	do.MustAs[*Locker, distributedlock.Locker](i)
}

// RegisterScopedLocker registers this implementation's scoped locker under two
// keys: its own type, *ScopedLocker, and distributedlock.ScopedLocker. Both
// resolve to the same locker.
//
// It is registered apart from RegisterLocker because the two are not layers of
// one thing: the scoped locker waits in the database on
// pg_advisory_xact_lock rather than acquiring a lease and refreshing it, so a
// service that wants both gets two independent objects either way.
//
// Prerequisites, arguments, and observability are as for RegisterLocker.
func RegisterScopedLocker(i do.Injector, cb circuitbreaking.CircuitBreaker, opts ...Option) {
	do.Provide(i, func(i do.Injector) (*ScopedLocker, error) {
		pillars, err := observability.InvokePillars(i)
		if err != nil {
			return nil, err
		}

		return NewPostgresScopedLocker(
			do.MustInvoke[*Config](i),
			do.MustInvoke[database.Client](i),
			cb,
			append([]Option{
				WithLogger(pillars.Logger),
				WithTracerProvider(pillars.TracerProvider),
				WithMetricsProvider(pillars.MetricsProvider),
			}, opts...)...,
		)
	})

	// Cannot fail: *ScopedLocker implements distributedlock.ScopedLocker — the
	// compiler says so at the top of scoped.go — and the service it aliases was
	// just provided.
	do.MustAs[*ScopedLocker, distributedlock.ScopedLocker](i)
}
