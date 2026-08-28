/*
Package queries is the session schema described as data: the table's name, its
columns in the order every statement lists them, the subset a write may assign,
and the one column that may be NULL.

It exists because those facts have two consumers that must not disagree. The
generator behind `make generate` renders them through database/querygen into the
canonical .sql files sqlc is run over; the backend executes the querier
sqlc-gen-unison generates from those same files. A column list spelled in both
places could differ in one name, and the symptom would be a check that passes
over SQL nobody executes.

So it is spelled once, here. The .sql files beside this one are the generator's
output — see [Render] and sessions/database/internal/queriesgen — and why they
are committed at all, when the generated Go carries the same statements in
executable form, is sessions/database's package comment.

# Six statements, and none of them standard

querygen.Generator.StandardCRUD emits the set a conventional table gets, and
this table gets none of it. It carries no convention triple: no archived_at,
because a sweeper deletes expired rows outright and a soft delete would either
do nothing or defeat that; no last_updated_at, because the column a read moves
here is last_seen_at, a liveness signal the store's policy compares against
rather than a record of the row's last mutation. Without archived_at there is
nothing for the standard archive to stamp, and without a filter window there is
no list to page.

What is left is six statements, each rendered from one of querygen's keyed forms
and each named for what it does to a session rather than for the shape it came
from — see [Render], where they are listed in the order a session goes through
them.

Two of the six are worth knowing about before reading the .sql.

The create is an insert-ignore rather than a plain insert, so that a duplicate
identifier leaves zero rows affected instead of raising. That is what makes
sessions.ErrIDConflict reportable without parsing three drivers' errors, and
what keeps a collision inside Rename's transaction from taking the transaction
down with it.

The sweep binds its deadline instead of asking the server for the time. See
sweepDelete: expires_at is stamped from a clock the backend was handed, so a
comparison against the server's clock would be two clocks deciding one row.

# What this package does not describe

The scan side. The generated querier's row types carry it, derived from the same
column lists, which is the point: a column renamed in a migration is a failed
`make unison` rather than a runtime scan error on whichever dialect noticed
first.
*/
package queries
