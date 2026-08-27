/*
Package sqltier is where every package in this module that holds SQL is named
with the tier its statements execute on, and it holds nothing else.

One tier executes SQL here: a package renders its statements into a canonical
.sql, sqlc checks them against that package's own schema on each dialect it
serves, and the store executes the querier the generator emits. A column renamed
in a migration is a failed generate rather than a runtime scan error.

"Every package" is a claim, and a claim about a set is only checkable if the
exceptions to it are enumerated. An exemption nobody wrote down is
indistinguishable from a package somebody missed — both look like silence — and
the reader who notices reconstructs the list by grepping the module for SELECT,
which is a survey with a shelf life of one branch. The reasoning behind each
ruling lives in database/querygen's doc, beside the tier it is a ruling about.
What lives here is the enumeration, in the one form that cannot quietly fall out
of date: a test that finds the SQL itself and fails on a package no one has
classified.

Four answers, and each is checked rather than recorded:

	unison   the statements are in a checked corpus, executed through the
	         generated querier
	porting  the package still composes its SQL in Go, and its port is tracked
	exempt   the SQL is not table SQL, so the corpus has nothing to say about
	         it — a catalog read, a DCL grant, an advisory lock, a table whose
	         DDL is a runtime product of configuration
	none     the package holds no SQL at all, and is named because a survey
	         once said it did

The last one earns its place because a survey counted a keyword in a comment.
Recording "this package holds no SQL" as an assertion rather than as an absence
is what stops the next survey from re-deriving the same false positive, and what
turns a package that later grows a statement into a failing test rather than an
unnoticed thirteenth store.

Discovery is deliberately crude: the test parses every non-test Go file in the
module and asks whether any string literal opens with a SQL statement keyword in
upper case, which is how this module writes them. That finds a package by the
statements it holds rather than by anything it chooses to declare — the property
that matters, since a package that has stopped registering itself is exactly the
one to catch. It is a floor rather than a ceiling: a package assembling a
statement out of pieces none of which opens with a keyword would go unfound, and
a test file's SQL is ignored, because the ruling is about what a package
executes in production rather than what its container tests set up.
*/
package sqltier
