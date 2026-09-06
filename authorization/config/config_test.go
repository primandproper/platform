package authorizationcfg

import (
	"testing"
	"time"

	"github.com/primandproper/platform-go/v14/authorization"
	"github.com/primandproper/platform-go/v14/authorization/cached"
	"github.com/primandproper/platform-go/v14/cache"
	"github.com/primandproper/platform-go/v14/cache/memory"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

const (
	permRead  authorization.Permission = "read.things"
	permWrite authorization.Permission = "write.things"
)

func build(t *testing.T, cfg *Config) (authorization.PolicyResolver, error) {
	t.Helper()

	return NewPolicyResolver(
		t.Context(),
		cfg,
		nil,
	)
}

func newCache(t *testing.T) cache.Cache[authorization.PermissionSet] {
	t.Helper()

	c, err := memory.NewInMemoryCache[authorization.PermissionSet](0)
	must.NoError(t, err)

	return c
}

// The default is what most users get, so "runs with no infrastructure" and
// "denies when unconfigured" are asserted together — neither can regress
// without failing here.
func TestNewPolicyResolver_Default(T *testing.T) {
	T.Parallel()

	T.Run("a zero-value config builds without a database", func(t *testing.T) {
		t.Parallel()

		resolver, err := build(t, &Config{})

		must.NoError(t, err)
		test.NotNil(t, resolver)
	})

	T.Run("a nil config builds", func(t *testing.T) {
		t.Parallel()

		resolver, err := build(t, nil)

		must.NoError(t, err)
		test.NotNil(t, resolver)
	})

	T.Run("the default resolver denies everything", func(t *testing.T) {
		t.Parallel()

		resolver, err := build(t, &Config{})
		must.NoError(t, err)

		set, err := resolver.PermissionsForRoles(t.Context(), "admin")
		must.NoError(t, err)

		test.True(t, set.IsEmpty())
		test.False(t, authorization.NewGrants(set).Has(permRead))
	})
}

func TestNewPolicyResolver_Static(T *testing.T) {
	T.Parallel()

	T.Run("resolves roles loaded from config", func(t *testing.T) {
		t.Parallel()

		resolver, err := build(t, &Config{
			Roles: []authorization.Role{
				{Name: "member", Permissions: []authorization.Permission{permRead}},
				{Name: "admin", Permissions: []authorization.Permission{permWrite}, Inherits: []string{"member"}},
			},
		})
		must.NoError(t, err)

		set, err := resolver.PermissionsForRoles(t.Context(), "admin")
		must.NoError(t, err)

		test.True(t, set.HasAll(permRead, permWrite))
	})

	T.Run("rejects a malformed policy at construction", func(t *testing.T) {
		t.Parallel()

		_, err := build(t, &Config{
			Roles: []authorization.Role{{Name: "a", Inherits: []string{"a"}}},
		})

		test.Error(t, err)
	})

	T.Run("passes static options through", func(t *testing.T) {
		t.Parallel()

		resolver, err := NewPolicyResolver(t.Context(), &Config{}, nil, WithStaticOptions(nil))

		must.NoError(t, err)
		test.NotNil(t, resolver)
	})
}

// Supplying a cache is what turns a resolution from a lookup per call into a
// lookup per policy change, so the wrapping is worth asserting rather than
// assuming.
func TestNewPolicyResolver_Cached(T *testing.T) {
	T.Parallel()

	T.Run("wraps the resolver when a cache is supplied", func(t *testing.T) {
		t.Parallel()

		resolver, err := NewPolicyResolver(
			t.Context(),
			&Config{Roles: []authorization.Role{
				{Name: "member", Permissions: []authorization.Permission{permRead}},
			}},
			newCache(t),
		)
		must.NoError(t, err)

		_, isCached := resolver.(*cached.Resolver)
		test.True(t, isCached)

		set, err := resolver.PermissionsForRoles(t.Context(), "member")
		must.NoError(t, err)
		test.True(t, set.Has(permRead))
	})

	T.Run("honors a configured TTL", func(t *testing.T) {
		t.Parallel()

		resolver, err := NewPolicyResolver(
			t.Context(),
			&Config{CacheTTL: 90 * time.Second},
			newCache(t),
		)

		must.NoError(t, err)
		test.NotNil(t, resolver)
	})

	T.Run("returns the bare resolver when no cache is supplied", func(t *testing.T) {
		t.Parallel()

		resolver, err := build(t, &Config{})
		must.NoError(t, err)

		_, isCached := resolver.(*cached.Resolver)
		test.False(t, isCached)
	})

	// The process that edits policy reaches invalidation through the optional
	// interface, never through the concrete type: where the decorator sits in
	// the chain is this package's decision and may change.
	T.Run("a cached resolver is reachable as a PolicyInvalidator", func(t *testing.T) {
		t.Parallel()

		resolver, err := NewPolicyResolver(
			t.Context(),
			&Config{Roles: []authorization.Role{
				{Name: "member", Permissions: []authorization.Permission{permRead}},
			}},
			newCache(t),
		)
		must.NoError(t, err)

		invalidator, ok := resolver.(authorization.PolicyInvalidator)
		must.True(t, ok)

		must.NoError(t, invalidator.Invalidate(t.Context(), "member"))
		invalidator.InvalidateAll()

		// Still answers after invalidation — the entry is dropped, not the policy.
		set, err := resolver.PermissionsForRoles(t.Context(), "member")
		must.NoError(t, err)
		test.True(t, set.Has(permRead))
	})

	// An uncached resolver deliberately does not implement it, so the assertion
	// above is a real question rather than one that always answers yes.
	T.Run("an uncached resolver is not a PolicyInvalidator", func(t *testing.T) {
		t.Parallel()

		resolver, err := build(t, &Config{})
		must.NoError(t, err)

		_, ok := resolver.(authorization.PolicyInvalidator)
		test.False(t, ok)
	})
}

// NewCachedResolver is the seam authzdbcfg calls, so its two behaviors are
// asserted here rather than only through the other half.
func TestNewCachedResolver(T *testing.T) {
	T.Parallel()

	T.Run("hands the resolver back untouched when no cache is supplied", func(t *testing.T) {
		t.Parallel()

		bare, err := build(t, &Config{})
		must.NoError(t, err)

		wrapped, err := NewCachedResolver(&Config{}, bare, nil)
		must.NoError(t, err)

		test.True(t, wrapped == bare)
	})

	T.Run("wraps whatever it is handed", func(t *testing.T) {
		t.Parallel()

		bare, err := build(t, &Config{Roles: []authorization.Role{
			{Name: "member", Permissions: []authorization.Permission{permRead}},
		}})
		must.NoError(t, err)

		wrapped, err := NewCachedResolver(&Config{CacheTTL: time.Minute}, bare, newCache(t))
		must.NoError(t, err)

		_, isCached := wrapped.(*cached.Resolver)
		test.True(t, isCached)

		set, err := wrapped.PermissionsForRoles(t.Context(), "member")
		must.NoError(t, err)
		test.True(t, set.Has(permRead))
	})

	T.Run("a nil config is the zero config", func(t *testing.T) {
		t.Parallel()

		bare, err := build(t, &Config{})
		must.NoError(t, err)

		wrapped, err := NewCachedResolver(nil, bare, newCache(t))

		must.NoError(t, err)
		test.NotNil(t, wrapped)
	})
}

func TestConfig_ValidateWithContext(T *testing.T) {
	T.Parallel()

	T.Run("the zero value is valid", func(t *testing.T) {
		t.Parallel()

		test.NoError(t, (&Config{}).ValidateWithContext(t.Context()))
	})

	T.Run("a positive TTL is valid", func(t *testing.T) {
		t.Parallel()

		test.NoError(t, (&Config{CacheTTL: time.Minute}).ValidateWithContext(t.Context()))
	})

	// cached.WithTTL folds anything not positive into "leave the default", so a
	// negative is a deployment asking for something it would silently not get.
	T.Run("rejects a negative TTL", func(t *testing.T) {
		t.Parallel()

		test.Error(t, (&Config{CacheTTL: -time.Second}).ValidateWithContext(t.Context()))
	})

	T.Run("a negative TTL fails construction rather than defaulting", func(t *testing.T) {
		t.Parallel()

		_, err := build(t, &Config{CacheTTL: -time.Second})

		test.Error(t, err)
	})
}
