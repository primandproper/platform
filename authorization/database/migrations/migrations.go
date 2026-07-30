/*
Package migrations supplies the authorization policy tables' DDL, rendered for
a dialect and table prefix.

As in outbox/migrations, no numbered migration file ships here: migration
numbers are global per consumer, so a platform-owned number would collide with
the consumer's own the moment either side added one. The version is always the
consumer's to choose.

	ddl, err := migrations.SQL(dialect.Postgres, database.DefaultTablePrefix)
	// ...
	m, err := migrate.New(dialect.Postgres, myMigrations,
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

	"github.com/primandproper/platform-go/v8/database/dialect"
	platformerrors "github.com/primandproper/platform-go/v8/errors"
)

//go:embed postgres.sql
var postgresDDL string

//go:embed mysql.sql
var mysqlDDL string

//go:embed sqlite.sql
var sqliteDDL string

var (
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
func Statements(d dialect.Dialect, tablePrefix string) ([]string, error) {
	var ddl string

	switch d {
	case dialect.Postgres:
		ddl = postgresDDL
	case dialect.MySQL:
		ddl = mysqlDDL
	case dialect.SQLite:
		ddl = sqliteDDL
	default:
		return nil, platformerrors.Wrapf(dialect.ErrUnsupported, "authorization migration dialect %q", d)
	}

	if !validPrefix.MatchString(tablePrefix) {
		return nil, platformerrors.Wrapf(ErrInvalidTablePrefix, "prefix %q", tablePrefix)
	}

	return dialect.SplitStatements(strings.ReplaceAll(ddl, prefixPlaceholder, tablePrefix)), nil
}

// SQL renders the same DDL as Statements, joined back into one migration body
// for database/migrate's WithGeneratedMigration.
func SQL(d dialect.Dialect, tablePrefix string) (string, error) {
	stmts, err := Statements(d, tablePrefix)
	if err != nil {
		return "", err
	}

	return strings.Join(stmts, ";\n\n") + ";\n", nil
}
