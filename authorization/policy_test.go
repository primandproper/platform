package authorization

import (
	"errors"
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// hierarchyRoles mirrors the shape a real policy takes: a member role, an admin
// that inherits it, and a service admin that inherits the admin.
func hierarchyRoles() []Role {
	return []Role{
		{Name: "member", Permissions: []Permission{permRead}},
		{Name: "admin", Permissions: []Permission{permWrite}, Inherits: []string{"member"}},
		{Name: "service_admin", Permissions: []Permission{permDelete}, Inherits: []string{"admin"}},
	}
}

func TestValidateRoles(T *testing.T) {
	T.Parallel()

	T.Run("accepts a well-formed policy", func(t *testing.T) {
		t.Parallel()

		test.NoError(t, ValidateRoles(hierarchyRoles()...))
	})

	T.Run("accepts an empty policy", func(t *testing.T) {
		t.Parallel()

		test.NoError(t, ValidateRoles())
	})

	T.Run("rejects an unnamed role", func(t *testing.T) {
		t.Parallel()

		err := ValidateRoles(Role{Permissions: []Permission{permRead}})

		test.True(t, errors.Is(err, ErrEmptyRoleName))
	})

	T.Run("rejects a duplicate role", func(t *testing.T) {
		t.Parallel()

		err := ValidateRoles(Role{Name: "member"}, Role{Name: "member"})

		test.True(t, errors.Is(err, ErrDuplicateRole))
	})

	T.Run("rejects an undefined parent", func(t *testing.T) {
		t.Parallel()

		err := ValidateRoles(Role{Name: "admin", Inherits: []string{"nobody"}})

		test.True(t, errors.Is(err, ErrUnknownParentRole))
	})

	T.Run("rejects self-inheritance", func(t *testing.T) {
		t.Parallel()

		err := ValidateRoles(Role{Name: "admin", Inherits: []string{"admin"}})

		test.True(t, errors.Is(err, ErrSelfInheritance))
	})

	T.Run("rejects a two-role cycle", func(t *testing.T) {
		t.Parallel()

		err := ValidateRoles(
			Role{Name: "a", Inherits: []string{"b"}},
			Role{Name: "b", Inherits: []string{"a"}},
		)

		test.True(t, errors.Is(err, ErrInheritanceCycle))
	})

	T.Run("rejects a longer cycle", func(t *testing.T) {
		t.Parallel()

		err := ValidateRoles(
			Role{Name: "a", Inherits: []string{"b"}},
			Role{Name: "b", Inherits: []string{"c"}},
			Role{Name: "c", Inherits: []string{"a"}},
		)

		test.True(t, errors.Is(err, ErrInheritanceCycle))
	})

	// A diamond is not a cycle: two roles may inherit a common ancestor.
	T.Run("accepts a diamond", func(t *testing.T) {
		t.Parallel()

		test.NoError(t, ValidateRoles(
			Role{Name: "base"},
			Role{Name: "left", Inherits: []string{"base"}},
			Role{Name: "right", Inherits: []string{"base"}},
			Role{Name: "top", Inherits: []string{"left", "right"}},
		))
	})
}

func TestExpandInheritance(T *testing.T) {
	T.Parallel()

	T.Run("accumulates permissions up the chain", func(t *testing.T) {
		t.Parallel()

		expanded, err := ExpandInheritance(hierarchyRoles()...)
		must.NoError(t, err)

		test.True(t, expanded["member"].Equal(NewPermissionSet(permRead)))
		test.True(t, expanded["admin"].Equal(NewPermissionSet(permRead, permWrite)))
		test.True(t, expanded["service_admin"].Equal(NewPermissionSet(permRead, permWrite, permDelete)))
	})

	// The containment invariant a policy author actually cares about, and the
	// one worth copying into a consumer's own test suite.
	T.Run("ancestors are subsets of descendants", func(t *testing.T) {
		t.Parallel()

		expanded, err := ExpandInheritance(hierarchyRoles()...)
		must.NoError(t, err)

		test.True(t, expanded["member"].IsSubsetOf(expanded["admin"]))
		test.True(t, expanded["admin"].IsSubsetOf(expanded["service_admin"]))
		test.False(t, expanded["service_admin"].IsSubsetOf(expanded["member"]))
	})

	T.Run("merges a diamond without double counting", func(t *testing.T) {
		t.Parallel()

		expanded, err := ExpandInheritance(
			Role{Name: "base", Permissions: []Permission{permRead}},
			Role{Name: "left", Permissions: []Permission{permWrite}, Inherits: []string{"base"}},
			Role{Name: "right", Permissions: []Permission{permDelete}, Inherits: []string{"base"}},
			Role{Name: "top", Inherits: []string{"left", "right"}},
		)
		must.NoError(t, err)

		test.True(t, expanded["top"].Equal(NewPermissionSet(permRead, permWrite, permDelete)))
	})

	T.Run("declaration order does not matter", func(t *testing.T) {
		t.Parallel()

		roles := hierarchyRoles()
		forward, err := ExpandInheritance(roles...)
		must.NoError(t, err)

		reversed := []Role{roles[2], roles[1], roles[0]}
		backward, err := ExpandInheritance(reversed...)
		must.NoError(t, err)

		for name, set := range forward {
			test.True(t, set.Equal(backward[name]))
		}
	})

	T.Run("validates before expanding", func(t *testing.T) {
		t.Parallel()

		expanded, err := ExpandInheritance(
			Role{Name: "a", Inherits: []string{"b"}},
			Role{Name: "b", Inherits: []string{"a"}},
		)

		test.Nil(t, expanded)
		test.True(t, errors.Is(err, ErrInheritanceCycle))
	})

	T.Run("a role with no permissions expands to an empty set", func(t *testing.T) {
		t.Parallel()

		expanded, err := ExpandInheritance(Role{Name: "bystander"})
		must.NoError(t, err)

		test.True(t, expanded["bystander"].IsEmpty())
	})
}
