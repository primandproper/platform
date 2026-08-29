/*
Package search is the parent of this module's four search packages and holds no
code of its own. It exists so the question "which of these do I need?" has an
answer that does not require reading four package docs to find out that three of
them are not it.

	search/text         query an index by words
	search/vector       query an index by nearest neighbor
	search/sync         keep either one in step with the database
	search/pagination   turn a text search into a page an API can hand back

# The two indexes

[github.com/primandproper/platform-go/v13/search/text] and
[github.com/primandproper/platform-go/v13/search/vector] are deliberate mirrors:
both define an Index[T] over a metadata payload, both put each provider in a
subpackage selected by a config subpackage, both ship a noop and a mock. Text
searches by term and gives back a cursor; vector searches by embedding proximity
and gives back distances. Which one an application wants follows from whether it
has an embedding to search with — see
[github.com/primandproper/platform-go/v13/embeddings] — and applications that
want both run both, because a hybrid of the two is a composition rather than a
third index.

Neither of them owns index creation. Dimensions, distance metrics, analyzers and
mappings are settled at construction through each provider's Config, so the
interface is the four operations a request performs and not the shape of the
thing it performs them on.

# The two seams around them

An index is a second copy of data whose first copy is a database row, which
makes both of the remaining packages inevitable rather than optional.

[github.com/primandproper/platform-go/v13/search/sync] is the write side. The
index event is enqueued in the transaction that changed the row, through
[github.com/primandproper/platform-go/v13/outbox], and applied afterwards from a
consumer — so the two systems converge rather than diverging silently the first
time the second write fails. It targets either index.

[github.com/primandproper/platform-go/v13/search/pagination] is the read side. A
text index's cursor and a
[github.com/primandproper/platform-go/v13/filtering.QueryFilter]'s cursor travel
in the same field and are not the same model, and an index reports whether
another page exists without reporting how many results there are in all. Both
facts have an obvious wrong handling; this package is where they are handled
once, along with the index-then-hydrate loop that a text search always is.

# What is not here

Retrieval-augmented generation, hybrid sparse-plus-dense retrieval and reranking
are compositions of these packages with
[github.com/primandproper/platform-go/v13/embeddings] and
[github.com/primandproper/platform-go/v13/llm]. They are an application's
pipeline, built from these parts, rather than a fifth package beside them.

Nor is there an HTTP surface. Search results are the application's own types
shaped for the application's own clients, so the handler is theirs — see the
module README's "Stores and Transports" section for where that line is drawn
across the module.
*/
package search
