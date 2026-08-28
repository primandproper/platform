/*
Package database stores authorization policy in SQL tables.

Reach for it when roles must be editable data — when an operator defines a new
role, or changes what an existing one grants, without shipping a release. If
the roles are fixed at build time, authorization/static answers the same
questions with no database and no migrations.

	ddl, err := migrations.SQL(dialect.Postgres, database.DefaultTablePrefix)
	// ... apply via database/migrate's WithGeneratedMigration

	resolver, err := database.NewResolver(
		&database.Config{Dialect: dialect.Postgres},
		client.Reader(),
		database.WithLogger(logger),
		database.WithTracerProvider(tracerProvider),
	)

# What it owns

Four tables: roles, permissions, the grants between them, and the inheritance
edges. Role *assignments* are deliberately absent. An assignment references the
consumer's own users and tenants, and a platform package cannot model those
without owning them too — so this package answers "what does this role grant"
and the consumer answers "which roles does this principal hold".

That split is also what makes caching worthwhile. Policy is keyed by role name,
so a deployment with five roles has five hot entries shared by every principal;
had this package owned assignments, the cache key would be the principal and
the hit rate would collapse. Wrap it in authorization/cached.

# Where the SQL comes from

Every statement this package runs is in a committed corpus. The tables and their
columns are declared as data in internal/queries, rendered through
database/querygen into one .sql per dialect, checked by sqlc against the schema
migrations renders — with no database running — and executed through the querier
sqlc-gen-unison emits from those same files. A column renamed in a migration is
a failed generate rather than a runtime scan error, on every table and in all
three dialects.

What that replaced was thirteen statements built with fmt.Sprintf, the table
prefix interpolated and the bind markers numbered by hand. Nothing checked any
of it against the schema until a container test ran it, and hand-numbered
markers fail in a characteristic way: correct on Postgres, where the marker
carries its own number, and silently wrong on MySQL and SQLite, where it does
not.

# Resolution

One query. A recursive CTE walks each named role up to its ancestors, then joins
through to permissions. Postgres, MySQL 8.0+, and SQLite all support it.

It uses UNION rather than UNION ALL, which is what makes it terminate if the
hierarchy ever contains a cycle. Seed and UpsertRole reject cycles before they
can be written, but a table edited by hand has no such guard, and a query that
returns a slightly surprising union is a far better failure than one that hangs.

Archived roles and permissions are excluded at every join rather than only at
write time, so archiving a permission revokes it everywhere on the next
resolution without touching a single mapping row.

Neither property is the caller's to get wrong, and neither is visible in a diff
of the SQL. Both are rendered unconditionally by querygen.Generator.ClosureQuery
— the shape this query is the only instance of — so a corpus carrying a
resolution with one and not the other is not something anybody can write.

# Writing policy

Seed takes the same []authorization.Role that static.NewResolver takes. One
declaration, either compiled in or written to the database — which is what keeps
a code-side policy from drifting away from a database-side one, the failure this
backend would otherwise invite.

Writes take the caller's executor rather than holding their own, following
outbox.Writer.Enqueue, so a policy change commits with whatever else its
transaction did:

	err := client.WithTransaction(ctx, func(q database.Tx) error {
		return resolver.Seed(ctx, q, roles...)
	})

Seed is idempotent, validates the whole policy before writing anything, and
leaves roles it was not given alone — so it can run on every deploy without
clobbering roles an operator added. Within a role it clears and rewrites, so
removing a permission from a role's list actually revokes it.

Writing a role or a permission is one lookup for the whole batch and then a
converging write for each row that is actually missing or actually different. A
re-run of an unchanged policy writes nothing at all, so the tables and their
indexes are not churned — and an audit trail on them stays worth reading.

The lookup is what supplies the id, and it has to. The write converges on the
name, and only Postgres could hand back the id of the row it converged on: an
existing name is therefore written under the id it already carries, and only a
name nothing was found for is minted one. Binding a fresh id for a name already
taken would leave the caller holding an id no row has, since MySQL resolves the
collision on whichever unique key it hit.

Grants and inheritance edges are cleared and rewritten a row at a time. The
multi-row VALUES list that preceded them had no static text — its arity was the
caller's cardinality — so there was nothing for sqlc to check; what replaces it
costs a round trip per grant, inside the transaction the caller already opened.

# Archival

ArchiveRole soft-deletes, and the name stays reserved.

A principal may still hold an assignment naming an archived role; resolution
simply stops finding it, so the assignment decays to granting nothing. Freeing
the name for reuse would instead re-grant whatever a new role of that name holds
to everyone still carrying the old assignment — a quiet privilege escalation
that would look like a naming coincidence.
*/
package database

//go:generate go run ./internal/queriesgen
