/*
Package queries is the notifications schema described as data: the canonical
table names, each table's columns in the order every read projects them, and the
subsets a write may assign.

It exists because those facts have two consumers that must not disagree. The
generator behind `make generate` renders these tables through database/querygen
into the canonical .sql files sqlc is run over; the store reads the same table
names to render its prefixed identifiers. A column list spelled in both places
could differ in one name, and the symptom would be a check that passes over SQL
nobody executes.

So it is spelled once, here, and both halves read it. The .sql files beside this
file are the generator's output — see [Render] and
notifications/internal/queriesgen.

# Why neither table takes the standard set

[querygen.Generator.StandardCRUD] emits the set a conventional table gets: reads
and writes keyed on the row's own id and, where the caller names one, on an
ownership column. Both tables here want more than that, for different reasons.

The inbox has two owners. A notification belongs to a scope and, within the
scope, to one person, and both are load-bearing: a get keyed on the scope alone
would let any member of a tenant read any other member's notification by id. So
every inbox statement is a keyed variant naming both, which is what
[querygen.Match] is for, and the standard set is not merely unused but
unexpressible.

The registry is keyed on the token. A device token is minted by the provider and
identifies a handset rather than a row this package created, so the registration
converges on (platform, token) — an upsert whose conflict target is the unique
index the schema declares — and the provider feedback hook deletes by the same
pair with no scope in the predicate at all. The id is still there, because a
paged list walks a cursor over it and because a revocation names one device, but
it is not what the writes key on.

# What the column lists decide

querygen derives a statement's predicates from the column list it is handed,
which is why [Table.ColumnsExcept] exists: a statement that keys on something
other than the row's own id says so by handing over a list without the id, and
what it projects is a separate list. Two statements here use it — the bulk mark
as read, which keys on a person rather than a notification, and the token
deletion, which keys on the token.

The registry's shorter list is doing the same work at the table level. It carries
created_at and neither of the other two convention columns, so querygen emits no
archive, renders no archived predicate on any read, and gives its list no
include_archived toggle. That is the schema's decision surfacing as an absence of
statements rather than as a rule somebody has to remember: an invalidated token
is deleted, and there is no statement here that could leave a dead token visible
to a delivery path.

# The count that is not a statement

An inbox badge is a count of unread notifications, and this corpus has no COUNT.
It does not need one: every list querygen renders carries filtered_count and
total_count as subqueries on its rows, so the unread page and the number
describing it come back from one statement and one snapshot of the table. A
second round trip would count a table that had moved on since the page was read.
*/
package queries
