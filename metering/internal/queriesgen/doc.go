/*
Package main renders the canonical sqlc input for metering's two tables, and the
DDL those queries are checked against.

It is the generator behind `make unison` for this component, and it does the two
jobs that command needs done in one order: with -schema it prints one dialect's
DDL, rendered from metering/migrations at the empty table prefix, which is what
sqlc analyzes; with no flags it writes one .sql per dialect into
metering/internal/queries, which is what sqlc analyzes against that DDL.

The empty prefix is not a default worth overriding. sqlc resolves table names
when it runs, and an identifier is not a bind parameter in any of the three
engines, so the canonical names are the only ones it could read. The consumer's
real prefix is substituted once, at construction, by the generated package.
*/
package main
