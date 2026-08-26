/*
Command queriesgen writes the canonical sqlc input for the identity schema, one
file per dialect, from identity/internal/queries.

It is run by `make generate`, and the files it writes are checked in. Nothing
imports them and nothing executes them: they exist so that `sqlc compile` can
check the statements the identity store executes against the schema
identity/migrations renders, at build time, with no database running. The store
itself renders the same statements through database/querygen's Bound methods,
which is the same text with the consumer's table prefix on the name and the
argument references rewritten into bind markers.

Because the check is what the files are for, this command also renders the other
half of it: `-schema <dialect>` prints the DDL sqlc reads them against, so
.scripts/sqlc_compile.sh has no hand-written copy of a schema to keep in step
with identity/migrations.

	go run ./internal/queriesgen                 # writes internal/queries/<dialect>_generated.sql
	go run ./internal/queriesgen -schema sqlite  # prints the DDL to stdout
*/
package main
