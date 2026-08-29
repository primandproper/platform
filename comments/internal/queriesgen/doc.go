/*
Package main renders the canonical sqlc input for the comments table, one
.sql per dialect, from the table description in
comments/internal/queries.

It is run by `go generate ./comments/...` and by CI's generated-files check,
which regenerates and diffs. Nothing edits the output by hand: the answer to "the
SQL on this line is wrong" is to change the table description or database/querygen
and generate again.

With -schema it does the other half instead, printing the DDL for one dialect at
the empty table prefix. That is what .scripts/unison_generate.sh feeds sqlc as
the schema to analyze the statements against, so the schema sqlc checks and the
schema a consumer migrates with are one file rather than two that can drift.
*/
package main
