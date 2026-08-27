/*
Package schemaconvention is where the module's schema convention is asserted,
and it holds nothing else.

Every table in this module that stores consumer rows carries the same three
columns:

	created_at      NOT NULL, defaulted by the server
	last_updated_at NULL until something changes the row
	archived_at     NULL until the row is soft-deleted

The convention is what database/querygen reads a table's shape from. It decides
which statements a table gets: a table spelling its last-mutation column
updated_at receives no update, no updated_after/updated_before window and no
UpdatedAfter/UpdatedBefore support from filtering.QueryFilter — and receives them
silently, because a generator that emits fewer statements looks exactly like a
table that wanted fewer. A created_at with no DEFAULT is worse: the generated
create omits the column, passes sqlc compile, and dies on a not-null violation
the first time it runs.

Both failures are invisible per-package, which is why the assertion is not
per-package. This package's test names every schema-shipping table in the module
exactly once — as conventional or as exempt, with the exemption's reason — so a
new table that quietly skips the triple fails a test rather than passing thirteen
of them. It imports every migrations subpackage and is imported by nothing.

A table is exempt only for a reason that outlives whoever wrote it, and there are
two shapes. A table a sweeper keeps small — sessions, work queue items, outbox
messages, WebAuthn ceremony state, metering's ingest ledger — cannot carry a soft
delete, because archived_at there either does nothing or keeps the table growing
forever. A mapping row between two tables is not listed, filtered or soft-deleted
independently of its parents, so the triple would be three columns no statement
reads. audit_log_entries is exempt for reasons of its own, which the test spells
out where it names it.
*/
package schemaconvention
