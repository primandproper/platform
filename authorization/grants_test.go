package authorization

import (
	"testing"

	"github.com/shoenig/test"
)

func TestGrants_ZeroValue(T *testing.T) {
	T.Parallel()

	// The zero value has to deny: a Grants that was never populated is what a
	// missing extractor, a failed authentication, or an unset struct field
	// produces, and every one of those must fail closed.
	T.Run("denies everything", func(t *testing.T) {
		t.Parallel()

		var g Grants

		test.False(t, g.Has(permRead))
		test.False(t, g.HasAny(permRead, permWrite))
		test.True(t, g.IsEmpty())
	})
}

func TestNewGrants(T *testing.T) {
	T.Parallel()

	T.Run("ORs across sets", func(t *testing.T) {
		t.Parallel()

		g := NewGrants(NewPermissionSet(permRead), NewPermissionSet(permWrite))

		test.True(t, g.Has(permRead))
		test.True(t, g.Has(permWrite))
		test.False(t, g.Has(permDelete))
	})

	// This is the "service administrator acting on a tenant they do not belong
	// to" case. Dropping nils at construction is what makes it need no branch
	// at any call site — it is simply a one-set Grants.
	T.Run("drops nil and empty sets", func(t *testing.T) {
		t.Parallel()

		g := NewGrants(NewPermissionSet(permRead), nil, NewPermissionSet())

		test.True(t, g.Has(permRead))
		test.False(t, g.IsEmpty())
	})

	T.Run("all-nil input denies", func(t *testing.T) {
		t.Parallel()

		g := NewGrants(nil, nil)

		test.False(t, g.Has(permRead))
		test.True(t, g.IsEmpty())
	})

	T.Run("no sets at all denies", func(t *testing.T) {
		t.Parallel()

		test.True(t, NewGrants().IsEmpty())
	})
}

func TestGrants_AllowAllDenyAll(T *testing.T) {
	T.Parallel()

	T.Run("AllowAll permits anything", func(t *testing.T) {
		t.Parallel()

		g := AllowAll()

		test.True(t, g.Has("anything.at.all"))
		test.True(t, g.HasAll(permRead, permWrite, permDelete))
		test.False(t, g.IsEmpty())
	})

	T.Run("DenyAll permits nothing", func(t *testing.T) {
		t.Parallel()

		g := DenyAll()

		test.False(t, g.Has(permRead))
		test.True(t, g.IsEmpty())
	})
}

func TestGrants_Predicates(T *testing.T) {
	T.Parallel()

	T.Run("HasAll spans sets", func(t *testing.T) {
		t.Parallel()

		g := NewGrants(NewPermissionSet(permRead), NewPermissionSet(permWrite))

		test.True(t, g.HasAll(permRead, permWrite))
		test.False(t, g.HasAll(permRead, permDelete))
	})

	T.Run("HasAll with no permissions is vacuously true", func(t *testing.T) {
		t.Parallel()

		test.True(t, DenyAll().HasAll())
	})

	T.Run("HasAny needs a witness", func(t *testing.T) {
		t.Parallel()

		g := NewGrants(NewPermissionSet(permRead))

		test.True(t, g.HasAny(permDelete, permRead))
		test.False(t, g.HasAny(permDelete))
		test.False(t, g.HasAny())
	})
}

func TestGrants_Evaluate(T *testing.T) {
	T.Parallel()

	// This is the shape an introspection endpoint needs: an entry per requested
	// permission, including the false ones, so a client can tell "denied" from
	// "not asked".
	T.Run("reports an entry per requested permission", func(t *testing.T) {
		t.Parallel()

		g := NewGrants(NewPermissionSet(permRead))

		got := g.Evaluate(permRead, permWrite)

		test.MapLen(t, 2, got)
		test.True(t, got[permRead])
		test.False(t, got[permWrite])
	})

	T.Run("never returns nil", func(t *testing.T) {
		t.Parallel()

		got := DenyAll().Evaluate()

		test.NotNil(t, got)
		test.MapLen(t, 0, got)
	})

	T.Run("survives a principal with no membership in the active scope", func(t *testing.T) {
		t.Parallel()

		// Service-wide authority present, tenant-scoped authority absent —
		// the case that produced a nil-map dereference when the two scopes
		// were separate maps a caller had to comma-ok.
		var tenantScoped *PermissionSet
		g := NewGrants(NewPermissionSet(permRead), tenantScoped)

		got := g.Evaluate(permRead, permWrite)

		test.True(t, got[permRead])
		test.False(t, got[permWrite])
	})
}
