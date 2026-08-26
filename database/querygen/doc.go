/*
Package querygen emits sqlc input for tables shaped the way this module's row
conventions expect, in the dialect of whichever of the three databases this
module supports will run it.

The conventions are already load-bearing elsewhere. filtering.QueryFilter is a
window over created_at and last_updated_at, a cursor compared against id, and a
flag deciding whether archived_at rows count. search/sync's Scanner wants a
strictly ordered page of IDs and nothing else. database's soft delete is
archived_at rather than DELETE. None of that was ever written down as SQL, so
each consumer wrote the SQL themselves, once per table, and the conventions held
exactly as long as everyone remembered them — which is to say they held until
the first table where someone did not.

This package writes that SQL. Its first consumer is sqlc: the whole reason to
hand queries to a generator is that they are checked against the schema at build
time and come back as typed Go, so what comes out of [Generator.StandardCRUD]
and the fragment methods is text, to be written to a .sql file and fed to sqlc
alongside the schema. Nothing here assembles a query per request, and none of it
reads a schema.

Its second consumer is a driver — see "The same statements, executed" below.
That is the same text with the argument references rewritten into bind markers,
not a second rendering of it: the semantics a filtered read has are subtle
enough that two copies of them would be two chances to get them wrong.

# What a caller supplies

A dialect, through [For], which returns the [Generator] every emitter hangs off:

	queries := querygen.For(dialect.Postgres).StandardCRUD("widgets", columns)

Then a table name and its column list, in the order the emitted SELECTs should
list them. Everything else is read off the column set:

	created_at present      → the created_after/created_before window
	last_updated_at present → the updated_after/updated_before window
	archived_at present     → soft delete, and the include_archived toggle
	last_indexed_at present → the reindex scan search/sync reads through, and
	                          the bulk stamp that maintains it
	id                      → required by StandardCRUD; the cursor its list
	                          pages by, and every query's key

A query whose column is absent is not emitted, and a predicate whose column is
absent is not rendered. That is the point of deriving them: a table without
last_updated_at cannot end up with an Update that sets it, and a table with
archived_at cannot end up without an Archive.

last_indexed_at is the one that took two rounds to get right. Its presence has
always decided the reindex scan, and the column has always been database-owned —
excluded from the create and the update, so no caller can supply it. What was
missing was anything that wrote it: the scan walked a column the convention
forbade everyone from maintaining. MarkXAsIndexed is that write, emitted from
the same column list as the scan, and a searchsync.Syncer flushes ids into it
through searchsync.NewStampBuffer. The column, the query that reads it, and the
write that maintains it are one feature rather than three-quarters of one.

WithOmitted subtracts from that set, for a table whose rows are not addressable
the way it assumes — a child row written with its parent and never read on its
own. It cannot add: what comes out stays a subset of what the columns justify, so
the properties above survive a caller who reaches for it.

Two things a column list cannot say are said with options rather than guessed at.
WithNullable names the columns a write may set to NULL, which lives in the schema
this package never reads; WithDatabaseOwned and WithImmutable name the columns a
caller may not assign, which lives in the application. Guessing either produces
SQL that generates, compiles, and is wrong at runtime.

# Argument names

The emitted SQL binds sqlc arguments whose names are neither the Go field names
nor the query-parameter names. All three spellings exist and none of them can be
guessed from another, so they are written down here:

	filtering.QueryFilter    URL parameter      sqlc argument
	CreatedAfter             createdAfter       created_after
	CreatedBefore            createdBefore      created_before
	UpdatedAfter             updatedAfter       updated_after
	UpdatedBefore            updatedBefore      updated_before
	IncludeArchived          includeArchived    include_archived
	Cursor                   cursor             cursor
	MaxResponseSize          limit              result_limit

The bulk stamp binds one argument that is not a filter field at all: ids, the
list of row ids to mark as indexed.

# The same statements, executed

Not everything that wants these statements wants to generate them. A store that
serves many tables from one implementation knows its table and columns at
construction rather than at build time, and there is no .sql file to write.

What such a caller needs is the argument references spelled as bind markers
instead of sqlc references, and that is the only thing the [Bound] methods
change. Each of them calls the statement function [Generator.StandardCRUD]
calls and rewrites the references in what comes back — the same rewrite sqlc
performs on these statements before its generated code hands one to a driver.
So there is no second rendering to drift from the first: the archived toggle
admits rows the same way, the counts omit the cursor the same way, the
last_updated_at bounds admit NULL the same way, because there is one of each
and both consumers read it.

	get := querygen.For(dialect.Postgres).BoundGet("widgets", columns,
		querygen.Match{Column: querygen.BelongsToAccountColumn})

	args, err := get.Bind(map[string]any{
		querygen.IDColumn:               id,
		querygen.BelongsToAccountColumn: account,
	})

A [Bound] holds the statement and the names its placeholders stand for, so it is
rendered once and bound per execution. [Match] adds an equality predicate on a
column — a tenancy scope, an owner, the reference a child row hangs off — and
[Generator.BindFilter] fills in the filter arguments, which is the mapping
between filtering.QueryFilter's fields and the argument names above.

Two things about arguments are worth knowing before reading Bound.Args. A name
can appear more than once: the filter predicates are rendered once and spliced
into the SELECT and into both counts, so on the dialects whose markers are
positional the same value is bound once per appearance, while Postgres numbers
its markers and binds it once. Markers are numbered where they appear in the
finished statement, which is what makes a spliced fragment come out right. And a bound value is not always what the Go field holds — SQLite stores
timestamps as text and compares them as text, so BindFilter hands it the shape
time.DateTime spells rather than a time, and MySQL's LIMIT cannot
coalesce so BindFilter supplies the default the other two coalesce to. Both are
in BindFilter rather than in a caller for the same reason the SQL is here.

# Tables with no id

[Generator.StandardCRUD] requires an id column and the [Bound] methods do not,
and the asymmetry is the one place the two halves of this package genuinely
disagree about what a table has to look like.

StandardCRUD emits the list, and the list pages by keyset over the id: the cursor
predicate compares against that column, so it has to sort by creation time, and a
composite key is not a cursor without machinery this package does not have. The
single-row statements need no such thing. They need to address one row, and a
table whose primary key is (subject_type, subject_id) addresses one exactly by
naming both — which is what [Match] has always been for, an equality predicate on
a column, bound rather than interpolated. So the id predicate is rendered when
the column list has an id and not when it does not, exactly as the archived_at
predicate is, and [Generator.BoundGet], [Generator.BoundExists],
[Generator.BoundUpdate] and [Generator.BoundArchive] key a row on whatever it
actually keys on:

	get := querygen.For(dialect.Postgres).BoundGet("shredding_subject_keys", columns,
		querygen.Match{Column: "subject_type"},
		querygen.Match{Column: "subject_id"})

Four tables in this module are in that position, each with a natural key that
carries a meaning a surrogate id would not: audit_log_chains keys on its scope,
shredding_subject_keys on (subject_type, subject_id) — which is the constraint
enforcing one live key per subject, and so the difference between a shred that
works and one leaving half the ciphertext readable — metering_totals on
(subject, meter, period_start), and scheduled_timers on (timer_set, timer_key).

What that costs is worth stating rather than discovering. Those four execute
[Bound] statements with no canonical .sql counterpart, so nothing about them
passes through sqlc: the projection and the placeholders stop being
hand-maintained, because this package renders both, but the statements are never
checked against the schema at build time the way a generated one is. A column
renamed in a migration is a runtime error on those four tables and a failed
generate everywhere else, and the only thing that catches it first is their own
container tests. That is a narrower guarantee than the rest of this package
offers, and it is the price of a key that means something.

A statement that keys on nothing at all — no id in the column list and no [Match]
— is [ErrUnaddressableRow] rather than a statement whose WHERE clause is the
archived predicate alone. Reading one row by reading all of them is not a
degenerate read; it is a different query, and archiving through one empties a
table.

# The table registry

Some of what a consumer needs per table is not a query. The TRUNCATE an
integration suite runs between tests is a list of table names; so is a schema
inventory, or a check that every table has a migration. The list has to be
complete, because the symptom of a missing entry is not a failure where the
mistake was made — a table left out of that TRUNCATE is a test somewhere else
failing later, on rows the previous test left behind.

The obvious place to get the list is wherever the per-table code lives, and that
is the trap. A generator with one builder per table doubles as a table list right
up until one table stops needing a builder — because its SQL now comes from
somewhere else, or because it never came from a generator at all — and then the
list is short by one with nothing to say so. The list survives only if it is fed
by the table existing rather than by something choosing to emit its queries.

So [Generator.StandardCRUD] registers every table it emits for, [RegisterTable]
takes the ones it does not, and [RegisteredTables] reads the union back:

	querygen.RegisterTable("sessions", "webauthn_credentials")

	tables := querygen.RegisteredTables()

Two sources, one list. A consumer reading that list does not have to know which
tables came from where, and a table moving from one source to the other does not
change what comes out.

# include_archived actually includes archived rows

A filtered list's WHERE clause is FilterConditions in its entirety, not an
addendum bolted onto a WHERE the caller opened with archived_at IS NULL. The
distinction is the difference between a working toggle and a decorative one: a
query reading

	WHERE t.archived_at IS NULL
	  AND (NOT COALESCE(sqlc.narg(include_archived), false) OR t.archived_at IS NULL)

parses, runs, reports no error, and returns the same rows for either value of the
flag, because the first predicate has already decided. Owning the whole clause is
what makes that unrepresentable.

# The three dialects

Postgres, MySQL and SQLite each get SQL their own server parses, and the
difference is confined to five expressions: the case-insensitive substring
match, the byte-ordered comparison the reindex scan walks, the sentinel an unset
time bound coalesces to, the nullable boolean the archived toggle binds, and the
set membership the bulk stamp keys on. They live together in generator.go, as
unexported methods, so that what this package assumes about a server is one
screen rather than a grep for casts. Everything else — the statement shapes, the
query names, which queries a column list justifies — is the same text on all
three.

The set is closed at the type. [For] takes a dialect.Dialect and rejects one
outside dialect.Valid rather than emitting a plausible default, and the dialect
binds to the [Generator] rather than to each call, so a Postgres fragment cannot
be spliced into a MySQL statement. That matters more than it sounds: the failures
are asymmetric. COLLATE "C" in MySQL is a parse error, which is the good case;
ILIKE has no SQLite spelling at all, and the substitute folds a narrower set of
characters, which is a search that quietly misses rows.

What a consumer sees is one set of sqlc methods with one set of signatures
whichever dialect generated them, so the application code above them is written
once. Two exceptions, both from sqlc's own inference rather than from anything
here: the archived toggle carries a ::boolean on Postgres and cannot elsewhere,
because MySQL and SQLite have no boolean type to cast to; and the bulk stamp's
id set is a bound array on Postgres and a sqlc.slice expansion on the other two,
which changes what reaches the server and not the []string a caller passes.

# What each dialect asks of a schema

A table generated for SQLite has to store its timestamps the way SQLite's own
CURRENT_TIMESTAMP writes them — YYYY-MM-DD HH:MM:SS, UTC. SQLite has no date
type, so the filter window's comparisons are lexicographic over text, and text in
any other shape compares in an order that is not chronological. The other two
have real timestamp types and no such requirement.

A table generated for MySQL needs its id column to be something MySQL will index
as a key: TEXT cannot be a primary key there without a prefix length, so ids
belong in a VARCHAR. Nothing in this package enforces either of these; both are
schema decisions, and this package never reads the schema.

# The one place a dialect changes a signature

Everything above is a difference in SQL under a Go API that does not move. LIMIT
is the exception, and it is worth knowing about before choosing MySQL.

Postgres and SQLite take an expression after LIMIT, so an absent page size
coalesces to filtering.DefaultQueryFilterLimit and the generated parameter is a
pointer a caller may leave nil. MySQL takes an integer literal or a placeholder
and nothing else — COALESCE there is a parse error rather than a slower plan — so
its LIMIT binds the size and the generated parameter is a value. Leveling the
other two down to match would take a working default away from the dialects that
can express one in order to make a limitation uniform, which is the wrong way
round.

Nothing drifts by leaving them different: the default is filtering's constant
rather than a number written here, so the SQL and
filtering.QueryFilter.Normalize read the same one. What a MySQL consumer owes its
queries is that Normalize call — it turns an absent or zero page size into that
constant and clamps an oversized one, the same treatment the URL parameter gets.
A MySQL query handed a zero returns no rows, which is loud, rather than a page of
some other size.
*/
package querygen
