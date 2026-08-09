package memory

import (
	"github.com/primandproper/platform-go/v10/distributedlock"
	"github.com/primandproper/platform-go/v10/observability"

	"github.com/samber/do/v2"
)

// RegisterLocker registers this implementation under two keys: its own type,
// *Locker, and distributedlock.Locker. Both resolve to the same locker.
//
// The concrete key is the point. A caller who has chosen the in-memory locker
// can depend on the thing they chose rather than on the interface every
// provider shares, and so need not carry the handling that interface forces on
// them — there is no network to lose, no lease to expire while unreachable,
// and Ping cannot fail. What that type does *not* promise is any coordination
// beyond this process, which is why naming it is worth more than hiding it.
// The interface key is an alias rather than a second provider, so two
// collaborators do not each get their own map and lock each other out of
// nothing.
//
// The options are passed here rather than invoked: this package has no config
// type: its knobs live in distributedlockcfg, which imports this package and so
// cannot be imported back. Register through distributedlockcfg instead when the
// provider should be the config's to choose.
//
// Observability comes from the injector's pillars when it has any. A container
// that registers none still wires up — every pillar resolves to its noop — but
// one whose registered provider fails to build reports that rather than
// degrading to a locker that looks instrumented and records nowhere.
func RegisterLocker(i do.Injector, opts ...Option) {
	do.Provide(i, func(i do.Injector) (*Locker, error) {
		pillars, err := observability.InvokePillars(i)
		if err != nil {
			return nil, err
		}

		// The caller's options are applied after the pillars, so an explicit
		// WithMetricsProvider(nil) leaves that one component unmetered even in
		// a container that registered a provider.
		return NewLocker(append([]Option{
			WithLogger(pillars.Logger),
			WithTracerProvider(pillars.TracerProvider),
			WithMetricsProvider(pillars.MetricsProvider),
		}, opts...)...)
	})

	// Cannot fail: *Locker implements distributedlock.Locker — the compiler
	// says so at the top of memory.go — and the service it aliases was just
	// provided.
	do.MustAs[*Locker, distributedlock.Locker](i)
}
