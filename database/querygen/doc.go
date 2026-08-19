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

This package writes that SQL. It is a generator, not a runtime builder: the
whole reason to hand queries to sqlc is that they are checked against the schema
at build time and come back as typed Go, and a builder that assembles SQL at
runtime gives both of those up. What comes out of here is text, to be written to
a .sql file and fed to sqlc alongside the schema.

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
	id                      → required; the cursor and every query's key

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
