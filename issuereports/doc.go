/*
Package issuereports is the durable half of asking your users what is wrong: a
scoped table of what they told you, and where each report stands.

Every product grows this table. A form somewhere collects a category and some
free text, a row is written, and then somebody has to work through them — which
means a status, which means a lifecycle, which means the same four states and the
same guarded move between them written again in each application. What varies is
the catalog of categories and what a report can be about, and both of those are
opaque to this package.

# The lifecycle, and why it is guarded

A report is open, acknowledged, resolved or declined. It is born open; it can be
picked up; it stops in one of the two terminal states; and it reopens to open
rather than to acknowledged, because reopening is what happens when the
resolution turned out not to hold.

[Store.TransitionReport] takes the status the caller believed the report was in
as well as the one it should move to, and the statement requires the row to still
hold it. That is the difference between a queue two people can work and one they
cannot: without the guard, two triagers resolving the same report both succeed,
the second note overwrites the first, and nothing anywhere says so. With it, one
of them gets ErrStatusConflict and re-reads.

The moves the lifecycle admits are checked in Go before the statement runs, so a
nonsensical move is refused rather than merely failing to match; see
[Status.CanTransitionTo].

# Tenancy

Every read and write takes a tenancy.Scope, and there is no variant of anything
that omits one. A deployment with a single tenant passes tenancy.Global()
everywhere and behaves exactly as it would have without the column.

There is deliberately no cross-scope listing — see [Store] for what that costs
and why the alternative is worse.

# Personal data

The details are a sentence somebody typed, and nothing can promise a sentence
somebody typed names nobody. So this table meets the dataprivacy seam like any
other store of personal data, and issuereports/privacy ships the two halves: a
dataprivacy.Collector that returns what a subject filed, and a dataprivacy.Eraser
that destroys it.

The erasure is a hard delete rather than an anonymization, and the reason is that
there is nothing to anonymize down to. Stripping the reporter off a report leaves
the free text, which is the part that identifies people; keeping the text and
losing the reporter would be a worse outcome than either.

# Where the SQL comes from

The store executes no SQL this module has not checked against its own schema.
issuereports/internal/queries describes the table as data; a generator renders
that into one .sql per dialect; sqlc checks each against the DDL
issuereports/migrations ships; and sqlc-gen-unison turns the checked statements
into the typed querier the store calls. A column renamed in a migration is a
failed generate rather than a runtime scan error, on all three dialects at once.

	make generate   # re-renders internal/queries/<dialect>_generated.sql
	make unison     # re-renders the schema and the generated querier

# Getting the table

issuereports/migrations renders the DDL for a dialect and a table prefix. It
ships no numbered migration file, because migration numbers are global per
consumer; hand migrations.SQL to database/migrate's WithGeneratedMigration and
the table is created by your own migration run.
*/
package issuereports

//go:generate go run ./internal/queriesgen
