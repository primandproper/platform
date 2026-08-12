/*
Package searchpagination adapts a text index's cursor pagination to the
filtering.QueryFilter pagination an API hands back to clients, and runs the
index-then-hydrate loop that a text search always is.

The two paginations are not the same model. A QueryFilter cursor is the last
row's ID on the database path, and an index cursor is an opaque token that only
the backend which issued it can read. They travel in the same field because a
client treats both the same way — a string it hands back verbatim — and because
which one it is follows from whichever flag the client sent to pick the search
path (filtering.QueryKeySearchWithDatabase). What that sharing costs is spelled
out on FilterForDatabaseFallback and CursorRejected.

Nothing here knows about any particular domain. It knows the two shapes on
either side of the seam, which is the part every caller was writing again.

# The two bugs this package exists to make unwritable

An unset limit is one page of the backend's choosing, so a call site that builds
a textsearch.SearchRequest by hand and forgets the limit returns a page the
client cannot size, ask past, or recognize as short. Search takes the page size
from the filter, so there is no call site left with a limit to forget.

The index reports whether another page exists but not how many results there are
in all. Reporting the page size as the total is therefore the same statement as
"this is the entire result set", which is how a truncated first page gets served
as a complete answer. NewResult leaves the total at zero, meaning unknown, as the
database search path already does.

# Index-then-hydrate

A text index holds a search subset, not the domain object, so a search is never
one call: query the index, collect the hit IDs, read those rows back from the
store, and wrap the page with the index's cursor rather than the last row's ID.
Hydrated is that loop, written once. Callers that need to interleave anything
into it — a fallback when the index comes back empty, say — can assemble the
same page from Search and NewResult instead.

# Scope

Text search. search/vector pages differently and is not covered here.
*/
package searchpagination
