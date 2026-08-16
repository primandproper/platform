/*
Package migrations supplies the webhook tables' DDL, rendered for a dialect and table prefix.

The platform deliberately does not ship a numbered migration file. Migration
files are numbered globally per consumer, so a platform-owned number would
collide with the consumer's own the moment either side added one. The version is
therefore always the consumer's to choose.

If you already run database/migrate, hand SQL to WithGeneratedMigration and the
tables are created by your normal migration run — no DDL copied into your
repository, nothing to keep in sync as this package evolves:

	ddl, err := migrations.SQL(dialect.Postgres, webhooks.DefaultTablePrefix)
	// ...
	m, err := migrate.New(dialect.Postgres, myMigrations,
		migrate.WithGeneratedMigration(43, "create_webhooks_tables", ddl),
	)

Statements is the same DDL split into individually executable statements, for
callers running it some other way — a different migration tool, or a test that
just wants the tables.

# Adding the scope column to a deployed schema

The DDL here only ever creates: every statement is IF NOT EXISTS, so running it
against tables that already exist adds nothing. A deployment that created the
webhook tables before the tenancy scope existed therefore needs one migration of
its own, and it is the same three statements in every dialect:

	ALTER TABLE webhooks_endpoints  ADD COLUMN scope TEXT NOT NULL DEFAULT '';
	ALTER TABLE webhooks_deliveries ADD COLUMN scope TEXT NOT NULL DEFAULT '';
	CREATE INDEX webhooks_endpoints_scope_idx ON webhooks_endpoints (scope, id);

The default is doing the work. An existing row acquires the empty identifier,
which is tenancy.Global() — so an application whose events are global passes
tenancy.Global() at every call site and its existing endpoints keep receiving
exactly what they received before. An application whose endpoints belong to
accounts has a data migration rather than a schema one: the account each row
belongs to has to be written into the new column, and until it is, those
endpoints are global endpoints and will not match a dispatch in an account's
scope. Backfill before the deploy that starts passing real scopes, not after.

The rendering and prefix vetting live in database/ddl, shared with every other
schema-shipping package in this module.
*/
package migrations

import (
	_ "embed"

	"github.com/primandproper/platform-go/v11/database/ddl"
	"github.com/primandproper/platform-go/v11/database/dialect"
)

//go:embed postgres.sql
var postgresDDL string

//go:embed mysql.sql
var mysqlDDL string

//go:embed sqlite.sql
var sqliteDDL string

// schema is this package's DDL in each supported dialect.
var schema = ddl.Schema{
	Component: "webhooks",
	Postgres:  postgresDDL,
	MySQL:     mysqlDDL,
	SQLite:    sqliteDDL,
}

// Statements renders the DDL for the dialect against the given table prefix and
// splits it into individually executable statements, each table before its
// indexes.
func Statements(d dialect.Dialect, prefix string) ([]string, error) {
	return schema.Statements(d, prefix)
}

// ValidatePrefix reports whether prefix yields a legal SQL identifier for every
// table and index this package creates.
func ValidatePrefix(prefix string) error {
	return schema.ValidatePrefix(prefix)
}

// SQL renders the same DDL as Statements, joined back into one migration body.
// It is what you hand to database/migrate's WithGeneratedMigration, so the
// tables are created by the consumer's own migration run instead of being
// copied into their repository.
func SQL(d dialect.Dialect, prefix string) (string, error) {
	return schema.SQL(d, prefix)
}
