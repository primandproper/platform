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

	ddl, err := migrations.SQL(migrations.DialectPostgres, outbox.DefaultTableName)
	// ...
	m, err := migrate.New(migrate.DialectPostgres, myMigrations,
		migrate.WithGeneratedMigration(37, "create_outbox_messages", ddl),
	)

Statements is the same DDL split into individually executable statements, for
callers running it some other way — a different migration tool, or a test that
just wants the table.
*/
package migrations

import (
	_ "embed"
	"regexp"
	"strings"

	platformerrors "github.com/primandproper/platform-go/v8/errors"
)

//go:embed postgres.sql
var postgresDDL string

//go:embed mysql.sql
var mysqlDDL string

//go:embed sqlite.sql
var sqliteDDL string

// Dialect selects which DDL to render. It mirrors outbox.Dialect, which this
// package cannot import without a cycle (outbox's tests use these statements).
type Dialect string

const (
	// DialectPostgres renders PostgreSQL DDL, including partial indexes.
	DialectPostgres Dialect = "postgres"
	// DialectMySQL renders MySQL 8.0+ DDL, with inline non-partial indexes.
	DialectMySQL Dialect = "mysql"
	// DialectSQLite renders SQLite DDL.
	DialectSQLite Dialect = "sqlite"
)

// ErrUnsupportedDialect indicates a dialect with no DDL.
var ErrUnsupportedDialect = platformerrors.New("unsupported outbox migration dialect")

// ErrInvalidTableName indicates a table name that is not a plain SQL
// identifier. The name is interpolated into DDL, so it is restricted rather
// than escaped.
var ErrInvalidTableName = platformerrors.New("invalid outbox table name")

// tablePlaceholder is the token each .sql file uses for the table name.
const tablePlaceholder = "{{TABLE}}"

// Statements renders the DDL for the dialect against the given table name and
// splits it into individually executable statements, in dependency order
// (table first, then indexes).
func Statements(dialect Dialect, tableName string) ([]string, error) {
	var ddl string

	switch dialect {
	case DialectPostgres:
		ddl = postgresDDL
	case DialectMySQL:
		ddl = mysqlDDL
	case DialectSQLite:
		ddl = sqliteDDL
	default:
		return nil, platformerrors.Wrapf(ErrUnsupportedDialect, "dialect %q", dialect)
	}

	if !validIdentifier(tableName) {
		return nil, platformerrors.Wrapf(ErrInvalidTableName, "table %q", tableName)
	}

	ddl = strings.ReplaceAll(ddl, tablePlaceholder, tableName)

	// Comments come out before the split, not after. A '--' comment may contain
	// a semicolon — prose routinely does — and splitting first tears such a
	// comment in half, leaving its tail masquerading as SQL at the head of the
	// next statement.
	var stmts []string
	for raw := range strings.SplitSeq(stripComments(ddl), ";") {
		if stmt := strings.TrimSpace(raw); stmt != "" {
			stmts = append(stmts, stmt)
		}
	}

	return stmts, nil
}

// SQL renders the same DDL as Statements, joined back into one migration body.
// It is what you hand to database/migrate's WithGeneratedMigration, so the
// outbox table is created by the consumer's own migration run instead of being
// copied into their repository:
//
//	ddl, err := migrations.SQL(migrations.DialectPostgres, outbox.DefaultTableName)
//	// ...
//	m, err := migrate.New(migrate.DialectPostgres, myMigrations,
//		migrate.WithGeneratedMigration(37, "create_outbox_messages", ddl),
//	)
//
// The comments are already stripped, which matters: goose splits a migration
// into statements on semicolons, and a '--' comment containing one would be torn
// in half exactly as it was here before Statements learned to strip first.
func SQL(dialect Dialect, tableName string) (string, error) {
	stmts, err := Statements(dialect, tableName)
	if err != nil {
		return "", err
	}

	return strings.Join(stmts, ";\n\n") + ";\n", nil
}

// stripComments drops whole-line '--' comments and blank lines. It does not
// handle a '--' appearing after SQL on the same line, nor semicolons inside
// string literals; this package's own DDL contains neither, and the round-trip
// tests against real servers are what keep that true.
func stripComments(ddl string) string {
	var kept []string

	for line := range strings.SplitSeq(ddl, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" && !strings.HasPrefix(trimmed, "--") {
			kept = append(kept, line)
		}
	}

	return strings.Join(kept, "\n")
}

// identifier matches a name safe to interpolate as a table name: a bare
// identifier, optionally qualified by exactly one schema.
//
// This duplicates outbox.identifier deliberately. Only _test.go files in outbox
// import this package, so importing outbox from here would close a cycle
// through outbox_test.go. Keep the two patterns identical; the shared cases in
// TestValidIdentifier_matchesOutbox guard the drift.
var identifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)?$`)

// validIdentifier reports whether s is safe to interpolate as a table name.
func validIdentifier(s string) bool {
	return identifier.MatchString(s)
}
