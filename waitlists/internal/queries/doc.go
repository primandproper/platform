/*
Package queries is the waitlist schema described as data: the canonical table
names, each table's columns in the order every read projects them, and the
subsets a write may assign.

It exists because those facts have two consumers that must not disagree. The
generator behind `make generate` renders these tables through
[querygen.Generator.StandardCRUD] and the keyed forms beside it into the
canonical .sql files sqlc is run over; the store reads the same table names to
render its prefix. A column list spelled in both places could differ in one name,
and the symptom would be a check that passes over SQL nobody executes.

So it is spelled once, here, and both halves read it. The .sql files beside this
file are the generator's output — see [Render] and waitlists/internal/queriesgen.

# Which queries each table gets

[Lists] is the only table that gets the standard set. Its rows are addressed by
their own id within a scope, which is exactly what
[querygen.Generator.StandardCRUD] emits, and [Table.Options] is where it says the
three things a column list cannot:

  - Ownership is the scope column, so every emitted statement is keyed on it.
    It is named rather than inferred, because a table whose rows are readable
    across scopes and one whose rows are not look identical from the columns.
  - Updatable names the columns the standard update assigns — the name, the
    description and the closing time — and everything else assignable becomes
    immutable to it.
  - Omitted drops the exists query. Nothing asks whether a list is there without
    also wanting to know when it closes, since that is the question every signup
    begins with.

[Signups] gets none of the standard set, and declares no Updatable to go with it.
Every single-row statement over that table keys on the list as well as on the
row's own id, which the standard set has no way to express, and its three writes
assign three different sets for three different reasons — a note edit, a
lifecycle transition, and the erasure a withdrawal performs. A single Updatable
list would be the union of the three, which is the set no statement wants.

# The keyed variants

A table's standard queries are not all of what a store runs against it, and the
difference used to be the hand-written half everywhere in this module. Each of
these is rendered here, through querygen's keyed forms, which are the standard
statements with more predicates rather than a second rendering of them:

  - the page of lists still taking signups, which is the catalog page with one
    more comparison — see [openListReads] for why that comparison is against a
    bound instant rather than the server's clock
  - the read-back of created_at on both tables, which no create carries because
    the database owns the column
  - the signup read keyed on the list and the row's id, and the one keyed on the
    digest of a contact, which is the only statement here rendered to see
    archived rows because the uniqueness it checks covers them
  - the two paged signup lists — one per list, one per subject
  - the insert, the three guarded updates and the archive

# The guard on a transition

[ExpectedStatusArg] is what makes a lifecycle move happen once. Both transition
statements name the status column twice — in the SET that assigns it and in the
predicate that requires the row to be in a particular state — and the two ends
bind different arguments, so the write cannot set the column to the value it was
requiring it to already hold. The affected-row count is the answer: two requests
inviting the same signup both find it waiting, and only one of their updates
reports a row.
*/
package queries
