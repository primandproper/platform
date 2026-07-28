package authorization

import (
	"context"
	"slices"
)

// Grants is a principal's effective authority for a single request: one or more
// granted permission sets, OR'd together.
//
// It is a struct rather than an interface because it sits on the hot path.
// Has is one or two map lookups with no allocation and no dynamic dispatch,
// which is what allows the check to stay synchronous and infallible while the
// policy behind it is pluggable and may do I/O.
//
// It holds a slice of sets rather than a materialized union deliberately.
// Merging a service-wide set and an account-scoped set of a few hundred
// permissions each would allocate a map of their combined size on every
// request; OR-ing at lookup allocates nothing.
//
// The zero value denies everything, so a Grants that was never populated — a
// missing extractor, a failed authentication, a struct field nobody set — is
// safe by construction rather than by remembering to check.
type Grants struct {
	sets []*PermissionSet
	all  bool
}

// GrantsExtractor pulls a principal's authority out of a request context.
//
// It is a function rather than an interface so that platform-go never needs to
// know how a consumer represents a session. The consumer writes the adapter
// over whatever its authentication layer put in the context, and that adapter
// is where a multi-scope model collapses into the flat "these sets, OR'd" the
// platform understands.
//
// Returning false means "no authority could be determined", which every
// enforcement path treats as a denial — not as an error, and never as a pass.
type GrantsExtractor func(ctx context.Context) (Grants, bool)

// NewGrants builds Grants from one or more permission sets. Nil sets are
// dropped.
//
// Dropping nils is what makes the awkward case structural rather than
// conditional: a service administrator acting on an account they are not a
// member of simply has one set instead of two. Callers do not check for it,
// and cannot forget to.
func NewGrants(sets ...*PermissionSet) Grants {
	kept := make([]*PermissionSet, 0, len(sets))
	for _, s := range sets {
		if s != nil && !s.IsEmpty() {
			kept = append(kept, s)
		}
	}
	if len(kept) == 0 {
		return Grants{}
	}

	return Grants{sets: kept}
}

// AllowAll returns Grants that permit everything.
//
// It exists for tests and local development, and it is deliberately a function
// call at a call site rather than a configurable provider: turning
// authorization off should be visible in code review, not reachable by setting
// an environment variable in production.
func AllowAll() Grants {
	return Grants{all: true}
}

// DenyAll returns Grants that permit nothing. It is the zero value, named.
func DenyAll() Grants {
	return Grants{}
}

// Has reports whether any of the granted sets contains p.
func (g Grants) Has(p Permission) bool {
	if g.all {
		return true
	}
	for _, s := range g.sets {
		if s.Has(p) {
			return true
		}
	}

	return false
}

// HasAll reports whether every permission in perms is granted. As with
// PermissionSet.HasAll, calling it with no permissions is vacuously true; see
// that method for why the guard belongs at the declaration site.
func (g Grants) HasAll(perms ...Permission) bool {
	for _, p := range perms {
		if !g.Has(p) {
			return false
		}
	}

	return true
}

// HasAny reports whether any permission in perms is granted.
func (g Grants) HasAny(perms ...Permission) bool {
	return slices.ContainsFunc(perms, g.Has)
}

// Evaluate reports the outcome for each permission in perms.
//
// This is the shape a "what can I do" introspection endpoint needs in order to
// tell a client which controls to render. The returned map is never nil and
// always has an entry for every requested permission, including the false ones
// — a caller distinguishing "denied" from "not asked" needs that distinction to
// survive.
func (g Grants) Evaluate(perms ...Permission) map[Permission]bool {
	out := make(map[Permission]bool, len(perms))
	for _, p := range perms {
		out[p] = g.Has(p)
	}

	return out
}

// IsEmpty reports whether these Grants permit nothing at all.
func (g Grants) IsEmpty() bool {
	if g.all {
		return false
	}
	for _, s := range g.sets {
		if !s.IsEmpty() {
			return false
		}
	}

	return true
}
