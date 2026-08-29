/*
Package scopeddl is where every tenancy column this module's schemas ship is
enumerated, and it holds nothing else.

The rule it enforces is one clause of the tenancy convention: a scope column
carries no DEFAULT. The empty string is a scope — tenancy.Global() — rather than
the absence of one, so a column that supplied it for a write which did not name
one hands the global scope to whoever forgot the column, and the row lands in the
tenant that matches nobody, unfindable by every scoped read. That is the mistake
tenancy.Scope exists to make unspellable in Go, and it is worth nothing at the
column if a writer that did not come through this module's stores can still make
it. NOT NULL with nothing to fall back on is what makes that write fail.

The clause is easy to lose because it reads as an omission. Every other text
column in these schemas defaults to the empty string, and the reviewer who adds a
tenancy column to a table where the neighbors all carry DEFAULT ” is adding the
one that must not, from a file that is telling them otherwise on every adjacent
line. Two packages' DDL predates the rule and had exactly that shape.

"Every schema" is a claim, and a claim about a set is only checkable if the set is
enumerated. The test here finds scope columns in the DDL itself — every .sql
under a migrations directory, the generated schema mirrors included — so a
package is found by the column it ships rather than by anything it declares, and
then checks the found set against the recorded one in both directions. A schema
that grows a tenancy column fails until somebody records it; a recorded one that
disappears fails too, so the enumeration cannot quietly become a list of columns
that used to exist.

Discovery is deliberately crude, in the same way internal/sqltier's is: the
tables are read out of the file text rather than through a parser, and a column
counts as a tenancy column when it is named scope or ends in _scope. That finds
subject_scope and leaves OAuth's scopes alone, which is the distinction that
matters — one is who the row belongs to, the other is a list of permissions. It
is a floor rather than a ceiling: a tenancy column under some third name would go
unfound, which is the enumeration's other job, since adding one means writing it
down here.

What this package does not check is the NOT NULL half, and the reason is that two
of these columns are a table's PRIMARY KEY, which implies it everywhere except
SQLite. Spelling that exception is a ruling about a different hazard — a NULL
scope rather than a defaulted one — and it belongs with whoever makes it.
*/
package scopeddl
