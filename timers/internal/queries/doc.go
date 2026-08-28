/*
Package queries is the timers schema described as data — the canonical table
name and the columns its statements touch — together with the statements the
timer set executes.

It exists because those facts have two consumers that must not disagree. The
generator behind `make generate` renders them into the canonical .sql that sqlc
is run over; the set executes the querier sqlc-gen-unison generates from that
same file. A column list spelled in both places could differ in one name, and
the symptom would be a check that passes over SQL nobody executes.

Why the rendered .sql is committed at all, when the generated Go beside it in
timers/internal/timersdb carries the same statements in executable form, is
identity's package comment, under "Where the SQL comes from".

# Postgres, and a roster of one

unison's dialect roster is the keys of unison.yaml's schemas map, so a
single-dialect package renders a single-dialect corpus and gets exactly the same
checked guarantee as a three-dialect one. What the roster does not do is soften
the requirement: every statement below is checked against the schema
timers/migrations renders, with no database running.

timers is Postgres-only because the claim is — the single statement that selects
due rows, locks them, extends the lease and hands them back is the concurrency
contract, and the SELECT-then-UPDATE it becomes without RETURNING is a different
failure model rather than a dialect switch. See the timers package comment. The
consequence here is narrow and worth stating plainly: a roster of one cannot
diverge, so RETURNING is available, and the claim reads its rows back in the
statement that leased them rather than in a second one.

# Everything is written out, and the line is not effort

Not one statement here comes from database/querygen, and the reason is the same
one in every case: this table has no id and no listing, and every write assigns
an expression rather than a bound value. querygen assigns a column the argument
it takes, with last_updated_at stamped by convention. The claim increments an
attempt counter and derives a lease horizon from a duration; the schedule
resolves a conflict through a CASE over whether an instant moved; the release
pushes an instant forward by an interval; the reap subtracts a retention window
from the server's own clock. A generator that could render those would be a
generator with an expression language in it, which is the thing querygen's
closed comparand set exists to refuse.

What the written-out statements do not give up is the guarantee, which is the
whole point of them being here rather than in the set's own package: each is a
complete statement in the committed corpus, checked by sqlc against this
package's own schema, and executed through the generated querier. A renamed
column is a failed `make unison` with no database running.

# A batch is arrays, not tuples

Four of the eight statements act on a batch whose size is decided at the call:
the schedule, the two keyed writes, and the cancel. A tuple list would make the
statement's text a function of the batch size, which is the dynamic SQL this
tier exists to replace — so a batch crosses the seam as one bound array per
column instead, and the statement is one fixed text however many timers are in
it.

Where a batch is one column wide, that is `= ANY(...)` and nothing more. Where it
is several — a schedule's key, instant and payload, or a firing's key and the
instant that fences it — the arrays are unnested WITH ORDINALITY and joined on
the position, so the nth element of each is one row again. The caller's
obligation is the one the pairing implies: the arrays are parallel, and the
store's own splitting is what keeps them so.

# The fence, and why a firing is not just a key

A firing is addressed by its key and the exact instant the claimant was handed.
That is what makes the reschedule race harmless: a retirement or a hand-back
carrying a stale run_at matches nothing, so a timer moved while it was being
fired keeps its new schedule instead of being marked against the old one. It is
the same "matches nothing" outcome a lapsed lease already produces, so it needs
no handling anywhere else.

# One clock

The database's now() decides everything about time except which instant a timer
was scheduled for. The lease horizon, the due comparison, the lateness a claim
reports, the retention window a reap subtracts — all of it is written and
compared server-side, and durations cross the seam as microsecond counts turned
into intervals. run_at is the single exception and is bound absolutely, because
it is the thing the caller actually meant; whether it has arrived is still the
server's answer.

That is also why the reap does not come from querygen's bounded prune. Its
horizon would be a ceiling the caller computed, which is the right seam for a
column the application stamped — and fired_at is stamped by the server. Under
the injected clock this package takes, a caller-computed horizon and a
server-stamped column are not merely skewed but arbitrarily far apart.
*/
package queries
