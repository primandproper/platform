/*
Command queriesgen writes the canonical sqlc input for the operations schema,
one file per dialect it serves, from operations/internal/queries.

It is run by `make generate`, and the file it writes is checked in. Nothing
imports it and nothing executes it: it exists so that `sqlc compile` can check
the statements the operations store executes against the schema
operations/migrations renders, at build time, with no database running. What the
store executes is the querier sqlc-gen-unison generates from that same file —
the same text with the consumer's table prefix substituted for {{prefix}} and
the argument references rewritten into bind markers.

One dialect, because operations serves one. The loop is still a loop over
Dialects, so the roster is a list rather than a decision written into the
control flow — a second dialect would be a schema and a line, not a rewrite.

Because the check is what the file is for, this command also renders the other
half of it: `-schema <dialect>` prints the DDL sqlc reads it against, so
.scripts/sqlc_compile.sh has no hand-written copy of a schema to keep in step
with operations/migrations. `make unison` calls it that way before running the
emitter over the pair.

	go run ./internal/queriesgen                   # writes internal/queries/<dialect>_generated.sql
	go run ./internal/queriesgen -schema postgres  # prints the DDL to stdout
*/
package main
