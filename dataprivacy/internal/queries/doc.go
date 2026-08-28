/*
Package queries is the dataprivacy schema described as data: the canonical table
name, its columns in the order every read projects them, and the arguments the
guarded writes bind their comparands under.

It exists because those facts have two consumers that must not disagree. The
generator behind `make generate` renders them through database/querygen into the
canonical .sql files sqlc is run over; the store reads the same argument names
when it binds a guard. Spelled in both places they could differ in one letter,
and the symptom would be a check that passes over SQL nobody executes.

The .sql files beside this one are the generator's output — see [Render] and
dataprivacy/internal/queriesgen. What the store executes is the querier
sqlc-gen-unison generates from them, in dataprivacy/internal/dataprivacydb.

# What the port changed about this store's behavior

Four things, and each is a consequence of the statements becoming static rather
than assembled.

A transition is two named statements rather than one builder taking a set of
source statuses and a destination. The three the state machine actually makes
are a confirmation, which records the operation now doing the work, and two
cancellations, which must not touch that column — so the difference between them
was never the source status the builder was parameterized on. See [transitions].

The status sets are gone, and completed_at says what they said. Every transition
into a terminal state writes it and nothing else does, and nothing moves out of
one, so "terminal" is completed_at IS NOT NULL and "still owed to somebody" is
its complement. See [sweeps], which also says why the bound-set spelling would
have been silently wrong on two of the three dialects.

created_at stays the service's clock, which is this table's one departure from
the module's convention and is argued in [InsertColumns]: it is the instant a
statutory window starts running, due_at is that instant plus the window, and two
clocks for the two ends of one deadline is an inconsistency in the field a
regulator asks about.

And a horizon comparison is inclusive. `due_at <= due_before` where the builder
wrote `<`, so a request due at exactly the instant the gauge asks about is
overdue rather than neither overdue nor pending — see [querygen.AtMostArgument], where
the boundary is stated once for every sweep in the module rather than per
statement.

# What the reads gained

A subject's request history is a filtered list rather than a cursor walk with a
count beside it: the created/updated windows, the archived toggle and the two
counts are querygen's, rendered from the same column list as the projection, and
the counts ride on the rows rather than arriving from a second statement that
counts a table which has moved on. The two readings of a scope — one scope, or
every scope a subject appears in — are two statements, because no bound value
turns an equality into "any".
*/
package queries
