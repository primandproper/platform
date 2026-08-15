/*
Package database stores WebAuthn ceremony state in a SQL table.

A ceremony spans two requests, and in a fleet those two requests land wherever
the load balancer sends them. This is the store that survives that: the row the
first replica wrote is the row the second one reads, and the second one deletes
it as it reads, so a challenge is answerable exactly once.

# Consume is one transaction

The read and the delete are a single transaction, and the delete's affected-row
count — not the read — decides who owns the ceremony. Two replicas answering a
replayed assertion at the same instant both find the row; only one of their
deletes reports a row, and the other is told the session is not there. Doing it
the other way round, or in two statements, leaves a window exactly as wide as
the verification that follows.

# The table is yours to create

migrations renders the DDL for a dialect and prefix. Nothing here creates a
table on its own: a library that ran DDL against a caller's database would be a
library that decided when a deployment's schema changed.

# The sweeper

Rows expire, but a table does not reclaim them. WithSweeper starts a background
delete of everything past its deadline; without it, and without a scheduler
calling Sweep, the table grows by one row per ceremony forever. Expiry itself
does not depend on the sweep — Consume refuses a row past its deadline whether
or not anything has removed it yet.
*/
package database
