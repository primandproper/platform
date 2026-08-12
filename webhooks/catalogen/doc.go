/*
Package catalogen derives a webhooks.Catalog from the constants that declare an
application's event types, so the catalog is generated from the source of truth
rather than hand-maintained beside it.

webhooks gates both subscription and dispatch on the catalog: an event type
outside it cannot be subscribed to and cannot be dispatched. That makes the
catalog the authoritative list of what an application publishes, and a
hand-maintained copy of an authoritative list is a copy that drifts.

The drift is not benign in either direction. A missing entry fails the dispatch
gate, and Dispatch runs inside the caller's transaction — so in an application
that enqueues events with the write that caused them, a forgotten catalog entry
is not a missing webhook, it is a failed write. An entry for an event nobody
publishes is the milder half: a checkbox in the subscription UI that can never
fire.

# Usage

	err := catalogen.Generate(catalogen.Options{
		Dir:        "internal/domain",
		OutputPath: "internal/domain/webhooks/catalog/catalog.go",
		Package:    "catalog",
	})

Every constant under Dir whose name ends in Suffix ("EventType" by default)
contributes its value as the event type and its doc comment as the description.
The generated file declares one var of type webhooks.Catalog, which is the value
WithCatalog takes.

Check is the same derivation without the write: it reports whether the committed
file still matches the constants, and names what changed. That is the assertion
worth running in CI. "The catalog can be regenerated" is true of any tree; "the
committed catalog matches the constants" is the thing a reviewer cannot verify by
eye across two dozen files, and it is what a stale catalog fails.

# One tree

Dir is a single tree, not a list, and that is a constraint rather than an
omission. An event type is a domain concept; one declared in a repository or a
transport handler is a layering mistake, and a generator that collected it there
would legitimize it. A consumer whose event types genuinely live in two trees
calls Generate twice, into two packages, and has to say so.

# The suffix is EventType, not ServiceEventType

Suffix defaults to the shortest form an application is likely to use because the
failure mode of collecting too little is severe and silent at generation time.
In the codebase this package was written against, most constants read
FooServiceEventType — but a dozen identity events (password changed, session
revoked, the email verification pair) drop the Service infix and are published
exactly like the rest. Collecting only the longer suffix left every one of them
out of the catalog and therefore failing the dispatch gate, which surfaced as
failed writes rather than as missing webhooks. HasSuffix on the shorter form
catches both.

# Doc comments as descriptions

The description a subscription UI renders beside each checkbox comes from the
constant's doc comment, with the constant's own name trimmed off the front:

	// OrderCreatedEventType fires when an order is placed.
	OrderCreatedEventType EventType = "order.created"

becomes

	"order.created": {Description: "Fires when an order is placed."}

This is the convention worth arguing about, and it earns its place on one
condition: the comments were written for a human reading the constant. Where
that holds, the UI text and the code comment cannot disagree, because they are
the same string. Where it does not — comments that read "the event type for
order creation", or no comments at all — the generated descriptions will be
worse than hand-written ones, and this package has no way to tell the
difference. An undocumented constant is generated with an empty description
rather than rejected: a blank label is visible in the UI and fixable by writing
the comment, whereas refusing to generate would take the dispatch gate down over
prose.

# Build time only

Nothing here runs on a request path. It parses source with go/parser rather than
loading a package with a type checker, so it can read a tree it does not import
and does not compile, and it touches the filesystem in both directions:
Generate creates the output file and its parent directories, Check reads the
committed one.
*/
package catalogen
