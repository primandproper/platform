/*
Package migrate provides the platform's standard database.Migrator: embedded
SQL migrations with the operational discipline consumers otherwise hand-roll —
an instance-based provider (no global goose state, so parallel tests never
race), and on Postgres a session advisory lock that serializes concurrently
booting replicas, with probe timeouts tightened so a waiting replica notices
the winner promptly instead of goose's leisurely default. Lock and unlock
timeouts are configurable via WithLockTimeout and WithUnlockTimeout.

# Writing a migration

Put plain SQL in a numbered file — 00001_add_users.sql — and embed the
directory. The leading number orders migrations and must be unique. That is the
whole contract: the `-- +goose Up` annotation is inserted for you if you omit
it, so nothing in a routine migration has to name the migration library.

Only Up is ever applied; this package exposes no Down, so a Down section is
inert if present. goose's remaining annotations still work when you need them:
fence a statement whose body contains semicolons (a PL/pgSQL function, a DO
block) between `-- +goose StatementBegin` and `-- +goose StatementEnd`, or the
splitter will tear it apart. New rejects an unfenced dollar-quoted body rather
than let that happen quietly. `-- +goose NO TRANSACTION` and `-- +goose ENVSUB`
also work, and stay above the inserted annotation.

Migrations are read and checked once, when New is called, so a malformed file
fails construction rather than the first Migrate.

# Generated migrations

A platform package that owns a table can render its own DDL, and
WithGeneratedMigration splices that text into the sequence as if it were a file
— so the table is created by your normal migration run instead of by DDL copied
into your repository. outbox/migrations is the first of these. The version stays
yours to pick, because numbering belongs to whoever owns the sequence; a version
a file on disk already uses fails New rather than the first Migrate.

# Locking

The advisory lock ID is derived from a caller-supplied lock key: deployments
sharing a database serialize on the same key, while schema-isolated parallel
tests pass distinct keys (their schema name) and migrate concurrently instead
of queueing on a global ID.

A schema-per-test harness cannot pass its schema name to WithLockKey, because
the Migrator is usually built before anyone knows which schema a given test
drew. WithSchemaScopedLockKey closes that gap by reading current_schema() off
the connection when Migrate runs — the schema the DSN's search_path resolves to,
and the one the migrations are about to write into:

	m, err := migrate.New(dialect.Postgres, migrations, migrate.WithSchemaScopedLockKey())

Deployments on the default schema all resolve to "public" and go on sharing one
lock; test schemas resolve to themselves and never contend. Without it,
schema-isolated tests queue on one global ID and their parallelism is only
apparent. testutils/containers/pgtest's Instance.Schema is the harness this
exists for. Databases cloned from a template need none of it: advisory locks are
per-database, and the migration ran once into the template.

Wire it through database/config by passing the Migrator to NewDatabase with
RunMigrations enabled, or call Migrate directly with any *sql.DB.
*/
package migrate
