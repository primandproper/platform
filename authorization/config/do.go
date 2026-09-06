package authorizationcfg

import (
	"context"

	"github.com/primandproper/platform-go/v14/authorization"
	"github.com/primandproper/platform-go/v14/cache"
	"github.com/primandproper/platform-go/v14/internal/injection"
	"github.com/primandproper/platform-go/v14/observability"

	"github.com/samber/do/v2"
)

// RegisterPolicyResolver registers an authorization.PolicyResolver with the
// injector, resolving policy from cfg.Roles. The cache is optional: a registered
// cache.Cache[authorization.PermissionSet] wraps the resolver in the cached
// decorator, and its absence means every resolution hits the underlying
// resolver, which is NewPolicyResolver's documented uncached behavior.
//
// This registration builds no store and resolves no database.Client, so a
// container running static policy needs neither. A container resolving policy
// from SQL registers authzdbcfg.RegisterPolicyResolver instead — the two provide
// the same key, so exactly one of them belongs in any given injector.
//
// Prerequisites: context.Context and *Config must be registered in the injector
// before the resolver is invoked.
func RegisterPolicyResolver(i do.Injector) {
	do.Provide(i, func(i do.Injector) (authorization.PolicyResolver, error) {
		pillars, err := observability.InvokePillars(i)
		if err != nil {
			return nil, err
		}

		permissionSets, err := injection.InvokeOptional[cache.Cache[authorization.PermissionSet]](i)
		if err != nil {
			return nil, err
		}

		return NewPolicyResolver(
			do.MustInvoke[context.Context](i),
			do.MustInvoke[*Config](i),
			permissionSets,
			WithPillars(pillars),
		)
	})
}
