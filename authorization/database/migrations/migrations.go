/*
Package migrations supplies the authorization policy tables' DDL, rendered for
a dialect and table prefix.

As in outbox/migrations, no numbered migration file ships here: migration
numbers are global per consumer, so a platform-owned number would collide with
the consumer's own the moment either side added one. The version is always the
consumer's to choose.

	ddl, err := migrations.SQL(migrations.DialectPostgres, database.DefaultTablePrefix)
	// ...
	m, err := migrate.New(migrate.DialectPostgres, myMigrations,
		migrate.WithGeneratedMigration(41, "create_authorization_policy", ddl),
	)

Statements is the same DDL split into individually executable statements, for
callers running it some other way.

The four tables hold policy — what each role grants — and nothing else. Role
*assignments* are deliberately absent: they reference the consumer's own users
and tenants, which this package cannot model without owning those tables too.
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

// Dialect selects which DDL to render. It mirrors database.Dialect, which this
// package cannot import without a cycle.
type Dialect string

const (
	// DialectPostgres renders PostgreSQL DDL.
	DialectPostgres Dialect = "postgres"
	// DialectMySQL renders MySQL 8.0+ DDL.
	DialectMySQL Dialect = "mysql"
	// DialectSQLite renders SQLite DDL.
	DialectSQLite Dialect = "sqlite"
)

var (
	// ErrUnsupportedDialect indicates a dialect with no DDL.
	ErrUnsupportedDialect = platformerrors.New("unsupported authorization migration dialect")

	// ErrInvalidTablePrefix indicates a prefix that would not produce plain SQL
	// identifiers. The prefix is interpolated into DDL, so it is restricted
	// rather than escaped.
	ErrInvalidTablePrefix = platformerrors.New("invalid authorization table prefix")
)

// prefixPlaceholder is the token each .sql file uses for the table prefix.
const prefixPlaceholder = "{{PREFIX}}"

// validPrefix admits an empty prefix and any run of identifier characters that
// does not begin with a digit, so that prefix+"roles" is always a plain
// identifier.
var validPrefix = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)?$`)

// Statements renders the DDL for the dialect against the given table prefix and
// splits it into individually executable statements, in dependency order:
// roles and permissions first, then the tables referencing them.
func Statements(dialect Dialect, tablePrefix string) ([]string, error) {
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

	if !validPrefix.MatchString(tablePrefix) {
		return nil, platformerrors.Wrapf(ErrInvalidTablePrefix, "prefix %q", tablePrefix)
	}

	ddl = strings.ReplaceAll(ddl, prefixPlaceholder, tablePrefix)

	// Comments are stripped before splitting: a '--' comment may contain a
	// semicolon, and splitting first tears it in half, leaving its tail
	// masquerading as SQL at the head of the next statement.
	var stmts []string
	for raw := range strings.SplitSeq(stripComments(ddl), ";") {
		if stmt := strings.TrimSpace(raw); stmt != "" {
			stmts = append(stmts, stmt)
		}
	}

	return stmts, nil
}

// SQL renders the same DDL as Statements, joined back into one migration body
// for database/migrate's WithGeneratedMigration.
func SQL(dialect Dialect, tablePrefix string) (string, error) {
	stmts, err := Statements(dialect, tablePrefix)
	if err != nil {
		return "", err
	}

	return strings.Join(stmts, ";\n\n") + ";\n", nil
}

// stripComments drops whole-line '--' comments and blank lines. It does not
// handle a '--' after SQL on the same line, nor semicolons inside string
// literals; this package's DDL contains neither.
func stripComments(ddl string) string {
	var kept []string

	for line := range strings.SplitSeq(ddl, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" && !strings.HasPrefix(trimmed, "--") {
			kept = append(kept, line)
		}
	}

	return strings.Join(kept, "\n")
}
