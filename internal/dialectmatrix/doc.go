/*
Package dialectmatrix is where the module's SQL dialect support matrix is
checked against the tree it describes, and it holds nothing else.

The matrix itself lives in the module README, under "SQL Dialect Support",
because the question it answers — "can I run this on MySQL?" — is asked before
any package has been imported, by somebody choosing a database rather than
reading a doc comment. Three packages narrow the three-dialect promise
(operations, timers, workqueue, all Postgres-only, each for the claim its own
doc explains), and before that section existed the narrowing was discoverable
only by reading those docs after already having chosen the package.

A matrix is a copy of facts that live somewhere else, which is the kind of
documentation that goes quietly wrong: a package grows a dialect, or loses one,
and the table goes on reading true. So the table is not maintained against the
tree by hand. This package parses it and compares each row against two
independent ground truths, neither of which anybody edits for the table's sake:

	the DDL a package ships    <pkg>/migrations/{postgres,mysql,sqlite}.sql
	the queriers it executes   queries_{postgresql,mysql,sqlite}_generated.go

Both directions are checked, because a matrix fails in two ways. A package that
ships DDL and appears in no row is the narrowing nobody wrote down — the state
this ticket existed to end. A row naming a package that ships none is a matrix
outliving its subject, which reads exactly like a live entry.

Discovery is deliberately crude, in the manner of internal/sqltier: the tree is
walked for directories named migrations and for files whose names sqlc-gen-unison
gives its per-dialect output. That finds a package by what it ships rather than
by anything it declares, which is the property that matters — a package that has
stopped matching its row is precisely the one to catch.

What it does not check is the constructors. Each narrowed package already tests
that its own New and its own migrations refuse a dialect they have no SQL for;
repeating that here would be a second copy of an answer rather than a check on
this one. The claim this package makes is narrower and is the one nothing else
makes: that the sentence in the README is still true.
*/
package dialectmatrix
