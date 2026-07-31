/*
Package migrations supplies the outbox table's DDL, rendered for a dialect and
table name.

The platform deliberately does not ship a numbered migration file. Migration
files are numbered globally per consumer, so a platform-owned number would
collide with the consumer's own the moment either side added one. The version is
therefore always the consumer's to choose.

If you already run database/migrate, hand SQL to WithGeneratedMigration and the
table is created by your normal migration run — no DDL copied into your
repository, nothing to keep in sync as this package evolves:

	ddl, err := migrations.SQL(dialect.Postgres, outbox.DefaultTableName)
	// ...
	m, err := migrate.New(dialect.Postgres, myMigrations,
		migrate.WithGeneratedMigration(37, "create_outbox_messages", ddl),
	)

Statements is the same DDL split into individually executable statements, for
callers running it some other way — a different migration tool, or a test that
just wants the table.
*/
package migrations

import (
	_ "embed"
	"strings"

	"github.com/primandproper/platform-go/v9/database/dialect"
	platformerrors "github.com/primandproper/platform-go/v9/errors"
)

//go:embed postgres.sql
var postgresDDL string

//go:embed mysql.sql
var mysqlDDL string

//go:embed sqlite.sql
var sqliteDDL string

// tablePlaceholder is the token each .sql file uses for the table name.
const tablePlaceholder = "{{TABLE}}"

// Statements renders the DDL for the dialect against the given table name and
// splits it into individually executable statements, in dependency order
// (table first, then indexes).
func Statements(d dialect.Dialect, tableName string) ([]string, error) {
	var ddl string

	switch d {
	case dialect.Postgres:
		ddl = postgresDDL
	case dialect.MySQL:
		ddl = mysqlDDL
	case dialect.SQLite:
		ddl = sqliteDDL
	default:
		return nil, platformerrors.Wrapf(dialect.ErrUnsupported, "outbox migration dialect %q", d)
	}

	if !dialect.ValidIdentifier(tableName) {
		return nil, platformerrors.Wrapf(dialect.ErrInvalidIdentifier, "outbox table %q", tableName)
	}

	return dialect.SplitStatements(strings.ReplaceAll(ddl, tablePlaceholder, tableName)), nil
}

// SQL renders the same DDL as Statements, joined back into one migration body.
// It is what you hand to database/migrate's WithGeneratedMigration, so the
// outbox table is created by the consumer's own migration run instead of being
// copied into their repository:
//
//	ddl, err := migrations.SQL(dialect.Postgres, outbox.DefaultTableName)
//	// ...
//	m, err := migrate.New(dialect.Postgres, myMigrations,
//		migrate.WithGeneratedMigration(37, "create_outbox_messages", ddl),
//	)
//
// The comments are already stripped, which matters: goose splits a migration
// into statements on semicolons, and a '--' comment containing one would be torn
// in half exactly as it was here before Statements learned to strip first.
func SQL(d dialect.Dialect, tableName string) (string, error) {
	stmts, err := Statements(d, tableName)
	if err != nil {
		return "", err
	}

	return strings.Join(stmts, ";\n\n") + ";\n", nil
}
