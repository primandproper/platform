/*
Package querygen emits sqlc input for tables shaped the way this module's row
conventions expect.

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

A table name and its column list, in the order the emitted SELECTs should list
them. Everything else is read off the column set:

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

# Postgres only

The emitted SQL uses COALESCE over sqlc.narg, an INTERVAL cast, a boolean cast,
and COLLATE "C" — none of which port unchanged. database/dialect names three
dialects and this package serves one of them. A second backend belongs here when
there is a second backend to serve, not before: an abstraction shaped around one
implementation and a guess is shaped around the guess.
*/
package querygen
