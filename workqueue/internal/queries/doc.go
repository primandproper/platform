/*
Package queries is the work queue schema described as data — the canonical
table name and the columns its statements touch — together with the statements
the queue executes.

It exists because those facts have two consumers that must not disagree. The
generator behind `make generate` renders them into the canonical .sql that sqlc
is run over; the queue executes the querier sqlc-gen-unison generates from that
same file. A column list spelled in both places could differ in one name, and
the symptom would be a check that passes over SQL nobody executes.

Why the rendered .sql is committed at all, when the generated Go beside it in
workqueue/internal/workqueuedb carries the same statements in executable form,
is identity's package comment, under "Where the SQL comes from".

# Postgres, and a roster of one

unison's dialect roster is the keys of unison.yaml's schemas map, so a
single-dialect package renders a single-dialect corpus and gets exactly the same
checked guarantee as a three-dialect one. What the roster does not do is soften
the requirement: every statement below is checked against the schema
workqueue/migrations renders, with no database running.

workqueue is Postgres-only because the claim is — the single statement that
selects due rows, locks them, increments the attempt counter, extends the lease
and hands them back is the concurrency contract, and the SELECT-then-UPDATE it
becomes without RETURNING is a different failure model rather than a dialect
switch. See the workqueue package comment. The consequence here is narrow and
worth stating plainly: there is no MySQL rendering to reconcile, so the
RETURNING split that a portable corpus would have owed is not a shape this
package has, and RETURNING is simply available — the claim reads its rows back
in the statement that leased them rather than in a second one.

# Everything is written out, and the line is not effort

Not one statement here comes from database/querygen, and the reason is the same
one in every case: this table has no id and no listing, and every write assigns
an expression rather than a bound value. querygen assigns a column the argument
it takes, with last_updated_at stamped by convention — a convention this table
does not even carry, since a swept queue has nothing to archive. The claim
increments an attempt counter and derives a lease horizon from a duration; the
enqueue resolves a conflict through a GREATEST, a LEAST and four CASEs over
whether the row it landed on was finished; the release pushes availability
forward by an interval; the reap subtracts a retention window from the server's
own clock. A generator that could render those would be a generator with an
expression language in it, which is the thing querygen's closed comparand set
exists to refuse.

What the written-out statements do not give up is the guarantee, which is the
whole point of them being here rather than in the queue's own package: each is a
complete statement in the committed corpus, checked by sqlc against this
package's own schema, and executed through the generated querier. A renamed
column is a failed `make unison` with no database running.

# A batch is arrays, not tuples

Five of the seven statements act on a batch whose size is decided at the call:
the enqueue and the four keyed writes. A tuple list — or a run of placeholders —
would make the statement's text a function of the batch size, which is the
dynamic SQL this tier exists to replace, so a batch crosses the seam as one
bound array per column instead and the statement is one fixed text however many
items are in it.

Where a batch is one column wide, that is `= ANY(...)` and nothing more, which
is every keyed write: an item is addressed by its key, and the queue-name
predicate that accompanies it is a single bound value. The enqueue is the one
statement carrying several columns per row — a key, a priority and a delay — so
its arrays are unnested WITH ORDINALITY and joined on the position, and the nth
element of each is one entry again. The caller's obligation is the one the
pairing implies: the arrays are parallel, and the queue's own splitting is what
keeps them so.

# One clock, and no exception

The database's now() decides everything about time here, without the single
exception a timer set has: a work queue names no instants at all. Lease
horizons, availability, completion, the retention window a reap subtracts and
the age the health read reports are all written and compared server-side, and
durations cross the seam as microsecond counts turned into intervals. Nothing in
this corpus binds a timestamp, in either direction, which is why the package it
serves is the one scheduling component in this module with no clock.Clock
option.

That is also why the reap does not come from querygen's bounded prune. Its
horizon would be a ceiling the caller computed, which is the right seam for a
column the application stamped — and completed_at is stamped by the server, by
the statement above it.
*/
package queries
