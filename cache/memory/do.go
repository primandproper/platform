package memory

import (
	"time"

	"github.com/primandproper/platform-go/v10/cache"
	"github.com/primandproper/platform-go/v10/observability"

	"github.com/samber/do/v2"
)

// RegisterCache registers this implementation under two keys: its own type,
// *Cache[T], and cache.Cache[T]. Both resolve to the same cache.
//
// The concrete key is the point. A caller who has chosen the in-memory cache
// can depend on the thing they chose rather than on the interface every
// provider shares, and so need not carry the handling that interface forces on
// them — nothing here is unreachable over a network, nothing returns
// cache.ErrUnavailable, and Flush needs no namespace. The interface key is an
// alias rather than a second provider, so a collaborator that takes a
// cache.Cache[T] and one that takes a *Cache[T] still share one cache and one
// shutdown.
//
// It is generic because a cache holds values of one concrete type; each cached
// type is registered separately. defaultExpiry and the options are passed here
// rather than invoked, because this package has no config type to invoke — its
// knobs live in cachecfg, which imports this package and so cannot be imported
// back. Register through cachecfg instead when the provider should be the
// config's to choose.
//
// Observability comes from the injector's pillars when it has any. A container
// that registers none still wires up — every pillar resolves to its noop — but
// one whose registered provider fails to build reports that rather than
// degrading to a cache that looks instrumented and records nowhere.
func RegisterCache[T any](i do.Injector, defaultExpiry time.Duration, opts ...Option) {
	do.Provide(i, func(i do.Injector) (*Cache[T], error) {
		pillars, err := observability.InvokePillars(i)
		if err != nil {
			return nil, err
		}

		// The caller's options are applied after the pillars, so an explicit
		// WithMetricsProvider(nil) leaves that one component unmetered even in
		// a container that registered a provider.
		return NewInMemoryCache[T](defaultExpiry, append([]Option{
			WithLogger(pillars.Logger),
			WithTracerProvider(pillars.TracerProvider),
			WithMetricsProvider(pillars.MetricsProvider),
		}, opts...)...)
	})

	// Cannot fail: *Cache[T] implements cache.Cache[T] — the compiler says so
	// at the top of memory.go — and the service it aliases was just provided.
	do.MustAs[*Cache[T], cache.Cache[T]](i)
}
