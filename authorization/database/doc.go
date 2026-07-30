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

# Writing policy

Seed takes the same []authorization.Role that static.NewResolver takes. One
declaration, either compiled in or written to the database — which is what keeps
a code-side policy from drifting away from a database-side one, the failure this
backend would otherwise invite.

Writes take the caller's executor rather than holding their own, following
outbox.Writer.Enqueue, so a policy change commits with whatever else its
transaction did:

	err := client.WithTransaction(ctx, func(q database.SQLQueryExecutor) error {
		return resolver.Seed(ctx, q, roles...)
	})

Seed is idempotent, validates the whole policy before writing anything, and
leaves roles it was not given alone — so it can run on every deploy without
clobbering roles an operator added. Within a role it clears and rewrites, so
removing a permission from a role's list actually revokes it.

Upserts are lookup-then-write in batches of a hundred rather than a
dialect-specific ON CONFLICT clause: three statements per batch regardless of
size, no RETURNING, nothing that differs across the three dialects. A row is
updated only when its description actually changed or it was archived, so a
re-run of an unchanged policy writes nothing.

# Archival

ArchiveRole soft-deletes, and the name stays reserved.

A principal may still hold an assignment naming an archived role; resolution
simply stops finding it, so the assignment decays to granting nothing. Freeing
the name for reuse would instead re-grant whatever a new role of that name holds
to everyone still carrying the old assignment — a quiet privilege escalation
that would look like a naming coincidence.
*/
package database
