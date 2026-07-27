/*
Package migrations supplies the outbox table's DDL as statements the consumer
splices into its own migration sequence.

The platform deliberately does not ship these as a database/migrate FS.
Migration files are numbered globally per consumer, so a platform-owned
numbered file would collide with the consumer's own numbering the moment either
side added one. Instead, Statements returns the DDL for a dialect and table
name, and the consumer pastes it into a migration at a number it controls:

	stmts, err := migrations.Statements(migrations.DialectPostgres, outbox.DefaultTableName)
*/
package migrations

import (
	_ "embed"
	"strings"

	platformerrors "github.com/primandproper/platform-go/v7/errors"
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

	var stmts []string
	for raw := range strings.SplitSeq(ddl, ";") {
		if stmt := stripComments(raw); stmt != "" {
			stmts = append(stmts, stmt)
		}
	}

	return stmts, nil
}

// stripComments removes leading comment lines and surrounding whitespace, so a
// statement is either real SQL or empty. Splitting on ';' is safe here only
// because this package's own DDL contains no semicolons inside literals or
// bodies.
func stripComments(raw string) string {
	var kept []string

	for line := range strings.SplitSeq(raw, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" && !strings.HasPrefix(trimmed, "--") {
			kept = append(kept, line)
		}
	}

	return strings.TrimSpace(strings.Join(kept, "\n"))
}

// validIdentifier reports whether s is safe to interpolate as a table name.
func validIdentifier(s string) bool {
	if s == "" {
		return false
	}

	for i, part := range strings.Split(s, ".") {
		if i > 1 || part == "" {
			return false
		}

		for j, r := range part {
			switch {
			case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '_':
			case j > 0 && r >= '0' && r <= '9':
			default:
				return false
			}
		}
	}

	return true
}
