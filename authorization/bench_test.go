package authorization

import (
	"fmt"
	"testing"
)

// benchSets builds two permission sets of the given size, mirroring the shape a
// real principal carries: service-wide authority and tenant-scoped authority.
func benchSets(size int) (first, second *PermissionSet) {
	a := make([]Permission, 0, size)
	b := make([]Permission, 0, size)
	for i := range size {
		a = append(a, Permission(fmt.Sprintf("service.perm_%04d", i)))
		b = append(b, Permission(fmt.Sprintf("tenant.perm_%04d", i)))
	}

	return NewPermissionSet(a...), NewPermissionSet(b...)
}

func BenchmarkGrants_Has(b *testing.B) {
	const size = 500

	serviceSet, tenantSet := benchSets(size)
	grants := NewGrants(serviceSet, tenantSet)

	// A hit in the first set is the cheapest path.
	b.Run("hit in first set", func(b *testing.B) {
		perm := Permission("service.perm_0250")
		for b.Loop() {
			boolSink = grants.Has(perm)
		}
	})

	// A hit in the second set pays a miss on the first — the cost of holding
	// sets separately instead of merging them.
	b.Run("hit in second set", func(b *testing.B) {
		perm := Permission("tenant.perm_0250")
		for b.Loop() {
			boolSink = grants.Has(perm)
		}
	})

	// A miss touches every set, so it is the worst case.
	b.Run("miss", func(b *testing.B) {
		perm := Permission("nobody.has.this")
		for b.Loop() {
			boolSink = grants.Has(perm)
		}
	})

	b.Run("single set", func(b *testing.B) {
		single := NewGrants(serviceSet)
		perm := Permission("service.perm_0250")
		for b.Loop() {
			boolSink = single.Has(perm)
		}
	})
}

// BenchmarkGrants_Construction is the measurement that justifies holding a
// slice of sets rather than materializing their union.
//
// NewGrants keeps pointers and OR-s at lookup; the union alternative allocates
// a map of the combined size. Since Grants is built once per request and
// checked a handful of times, paying the merge up front is the worse trade —
// this benchmark is what says so rather than the assertion in the doc comment.
func BenchmarkGrants_Construction(b *testing.B) {
	const size = 500

	serviceSet, tenantSet := benchSets(size)

	b.Run("NewGrants keeps both sets", func(b *testing.B) {
		for b.Loop() {
			grantsSink = NewGrants(serviceSet, tenantSet)
		}
	})

	b.Run("materialized union", func(b *testing.B) {
		for b.Loop() {
			setSink = serviceSet.Union(tenantSet)
		}
	})
}

func BenchmarkGrants_Evaluate(b *testing.B) {
	serviceSet, tenantSet := benchSets(500)
	grants := NewGrants(serviceSet, tenantSet)

	perms := []Permission{
		"service.perm_0001",
		"tenant.perm_0002",
		"nobody.has.this",
	}

	for b.Loop() {
		mapSink = grants.Evaluate(perms...)
	}
}

func BenchmarkExpandInheritance(b *testing.B) {
	// A chain deep enough that transitive expansion actually costs something.
	roles := make([]Role, 0, 16)
	for i := range 16 {
		role := Role{
			Name:        fmt.Sprintf("role_%02d", i),
			Permissions: []Permission{Permission(fmt.Sprintf("perm_%02d", i))},
		}
		if i > 0 {
			role.Inherits = []string{fmt.Sprintf("role_%02d", i-1)}
		}
		roles = append(roles, role)
	}

	for b.Loop() {
		expandedSink, _ = ExpandInheritance(roles...)
	}
}

var (
	boolSink     bool
	setSink      *PermissionSet
	grantsSink   Grants
	mapSink      map[Permission]bool
	expandedSink map[string]*PermissionSet
)
