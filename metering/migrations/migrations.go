/*
Package migrations supplies the usage metering tables' DDL, rendered for a
dialect and table prefix.

The platform deliberately does not ship a numbered migration file. Migration
files are numbered globally per consumer, so a platform-owned number would
collide with the consumer's own the moment either side added one. The version is
therefore always the consumer's to choose.

If you already run database/migrate, hand SQL to WithGeneratedMigration and the
tables are created by your normal migration run — no DDL copied into your
repository, nothing to keep in sync as this package evolves:

	ddl, err := migrations.SQL(dialect.Postgres, metering.DefaultTablePrefix)
	// ...
	m, err := migrate.New(dialect.Postgres, myMigrations,
		migrate.WithGeneratedMigration(41, "create_metering_tables", ddl),
	)

Statements is the same DDL split into individually executable statements, for
callers running it some other way — a different migration tool, or a test that
just wants the tables.
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

// prefixPlaceholder is the token each .sql file uses for the table prefix.
const prefixPlaceholder = "{{PREFIX}}"

// Statements renders the DDL for the dialect against the given table prefix and
// splits it into individually executable statements, each table before its
// indexes.
func Statements(d dialect.Dialect, prefix string) ([]string, error) {
	var ddl string

	switch d {
	case dialect.Postgres:
		ddl = postgresDDL
	case dialect.MySQL:
		ddl = mysqlDDL
	case dialect.SQLite:
		ddl = sqliteDDL
	default:
		return nil, platformerrors.Wrapf(dialect.ErrUnsupported, "metering migration dialect %q", d)
	}

	// The prefix is vetted rather than escaped, on the same terms as a table
	// name: it is interpolated into query text, not bound. Vetting the prefix
	// alone is not enough — every rendered name has to be a legal identifier too,
	// and a prefix ending in a character that is fine mid-identifier could still
	// produce one that is not.
	if err := ValidatePrefix(prefix); err != nil {
		return nil, err
	}

	return dialect.SplitStatements(strings.ReplaceAll(ddl, prefixPlaceholder, prefix)), nil
}

// TableSuffixes are the per-table suffixes appended to the prefix. Declared here
// rather than in the metering package so the DDL and the queries derive their
// names from one list.
var TableSuffixes = []string{"events", "totals"}

// ValidatePrefix reports whether prefix yields a legal SQL identifier for every
// table this package creates.
func ValidatePrefix(prefix string) error {
	if prefix == "" {
		return platformerrors.Wrap(dialect.ErrInvalidIdentifier, "empty metering table prefix")
	}

	for _, suffix := range TableSuffixes {
		if name := prefix + "_" + suffix; !dialect.ValidIdentifier(name) {
			return platformerrors.Wrapf(dialect.ErrInvalidIdentifier, "metering table %q", name)
		}
	}

	return nil
}

// SQL renders the same DDL as Statements, joined back into one migration body.
// It is what you hand to database/migrate's WithGeneratedMigration, so the
// metering tables are created by the consumer's own migration run instead of
// being copied into their repository.
//
// The comments are already stripped, which matters: goose splits a migration
// into statements on semicolons, and a '--' comment containing one would be torn
// in half.
func SQL(d dialect.Dialect, prefix string) (string, error) {
	stmts, err := Statements(d, prefix)
	if err != nil {
		return "", err
	}

	return strings.Join(stmts, ";\n\n") + ";\n", nil
}
