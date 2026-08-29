/*
Command queriesgen writes the canonical sqlc input for the outbox schema, one
file per dialect it serves, from outbox/internal/queries.

It is run by `make generate`, and the files it writes are checked in. Nothing
imports it and nothing executes it: it exists so that `sqlc compile` can check
the statements the Writer and the Relay execute against the schema
outbox/migrations renders, at build time, with no database running. What they
execute is the querier sqlc-gen-unison generates from those same files — the
same text with the consumer's table prefix substituted for {{prefix}} and the
argument references rewritten into bind markers.

Because the check is what the files are for, this command also renders the other
half of it: `-schema <dialect>` prints the DDL sqlc reads them against, so
.scripts/sqlc_compile.sh has no hand-written copy of a schema to keep in step
with outbox/migrations. `make unison` calls it that way before running the
emitter over the pair.

	go run ./internal/queriesgen                   # writes internal/queries/<dialect>_generated.sql
	go run ./internal/queriesgen -schema postgres  # prints the DDL to stdout
*/
package main
