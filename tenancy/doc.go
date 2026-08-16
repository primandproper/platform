/*
Package tenancy carries one dimension: whose data a row is.

A component that stores consumer data stores it for somebody. In a single-tenant
application that somebody is the application itself and the dimension is
invisible; in a multi-tenant one it is an account, an organization, or a
workspace, and every read that omits it is a cross-tenant read. Scope is that
dimension, and it is one type rather than a field each component names for
itself.

# Why it is not a string

Because the mistake this exists to prevent is a filter somebody forgot.

	// Before: nothing in the signature says this answers a global question.
	EndpointsForEvent(ctx, q, eventType)

	// After: there is no call that omits the scope.
	EndpointsForEvent(ctx, q, scope, eventType)

An owner identifier typed as string is indistinguishable from every other string
a component takes, so the compiler cannot tell "who owns this" from "what
happened" and the two can be passed in the wrong order. Worse, the absence of an
owner is expressible: the empty string reads as "no filter" in a query that
concatenates it, which is a read that returns every tenant's rows.

A Scope cannot be built from nothing. The zero value names nobody, Validate
rejects it, and Value refuses to bind it — so a query that lost its scope fails
instead of quietly widening. Saying "no owner" is possible, but it has to be
said: that is Global.

# Global

	dispatcher.Dispatch(ctx, q, &webhooks.Delivery{
		Scope:     tenancy.Global(),
		EventType: "system.reindexed",
		Payload:   body,
	})

Global is the scope of data that belongs to no tenant — an application whose
events are global, a fleet-wide sweep, a platform-level record. It is a scope
like any other and matches only itself: rows in it are not visible to a tenant
scope, and a tenant's rows are not visible to it.

It is stored as the empty owner identifier, which is what makes adoption
survivable. A component's scope column defaults to the empty string, so rows
written before the column existed are global rows, and a single-tenant
application that passes Global everywhere behaves exactly as it did before the
dimension existed.

Of is the other constructor, and it deliberately does not accept an empty
identifier: Of takes an ID the caller holds, and an empty one is a bug rather
than a request for the global scope. Say Global when you mean global.

# What a component owes

Three things, and the third is the one that gets skipped:

  - Scope in the column. A TEXT column that is NOT NULL and defaults to the
    empty string, beside the row's own identity — not encoded into another
    column's value. A composite key like "<accountID>:<eventType>" scopes by
    construction, which is why it is tempting, and it cannot be indexed,
    filtered, or enumerated as the two facts it is.

  - Scope in the query. Every predicate that reads or writes consumer data
    carries it, and the store binds Scope itself rather than a string derived
    from it, so an unset scope is a driver error rather than a wider result set.

  - No read path that omits it. Not "a scoped variant exists" — the unscoped
    variant must not be reachable, because the one caller who reaches for it is
    the one who has not thought about tenancy. A component's own machinery is
    the exception, and it is a narrow one: a delivery worker draining a queue
    across every tenant is not a consumer read, it is the component servicing
    itself, and those methods say so in their documentation.

# What is deliberately not here

Resolving a scope from a request, mapping one to an authorization decision, and
tenancy hierarchies are all absent. A Scope says whose row it is; it does not
say who is asking, or whether they may. Authorization is authorization's job,
and this type is not a capability — holding one is not permission to read what
it names.

Hierarchy is absent because depth is an application's decision: a two-level
user-inside-account model cannot express one level or three. Scope is one opaque
identifier for the same reason audit.Entry.Scope and dataprivacy.Subject.Scope
are, and an application that needs a path can put one in the identifier.
*/
package tenancy
