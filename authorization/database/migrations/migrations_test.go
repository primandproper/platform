package migrations

import (
	"errors"
	"strings"
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func allDialects() []Dialect {
	return []Dialect{DialectPostgres, DialectMySQL, DialectSQLite}
}

func TestStatements(T *testing.T) {
	T.Parallel()

	T.Run("renders every dialect", func(t *testing.T) {
		t.Parallel()

		for _, dialect := range allDialects() {
			stmts, err := Statements(dialect, "authz_")
			must.NoError(t, err)

			test.True(t, len(stmts) >= 4)
		}
	})

	T.Run("substitutes the prefix", func(t *testing.T) {
		t.Parallel()

		for _, dialect := range allDialects() {
			stmts, err := Statements(dialect, "custom_")
			must.NoError(t, err)

			joined := strings.Join(stmts, "\n")

			test.StrContains(t, joined, "custom_roles")
			test.StrContains(t, joined, "custom_permissions")
			test.StrContains(t, joined, "custom_role_permissions")
			test.StrContains(t, joined, "custom_role_hierarchy")
			test.StrNotContains(t, joined, prefixPlaceholder)
		}
	})

	T.Run("an empty prefix is valid", func(t *testing.T) {
		t.Parallel()

		stmts, err := Statements(DialectSQLite, "")
		must.NoError(t, err)

		test.StrContains(t, strings.Join(stmts, "\n"), "roles")
	})

	// Dependency order matters: the mapping tables reference roles and
	// permissions, so those must be created first.
	T.Run("creates referenced tables first", func(t *testing.T) {
		t.Parallel()

		for _, dialect := range allDialects() {
			stmts, err := Statements(dialect, "authz_")
			must.NoError(t, err)

			joined := strings.Join(stmts, "\n")

			rolesAt := strings.Index(joined, "CREATE TABLE IF NOT EXISTS authz_roles")
			permsAt := strings.Index(joined, "CREATE TABLE IF NOT EXISTS authz_permissions")
			mappingAt := strings.Index(joined, "CREATE TABLE IF NOT EXISTS authz_role_permissions")

			test.True(t, rolesAt >= 0)
			test.True(t, permsAt >= 0)
			test.True(t, mappingAt > rolesAt)
			test.True(t, mappingAt > permsAt)
		}
	})

	// Comments are stripped before splitting on semicolons: prose routinely
	// contains one, and splitting first tears the comment in half, leaving its
	// tail masquerading as SQL.
	T.Run("strips comments", func(t *testing.T) {
		t.Parallel()

		for _, dialect := range allDialects() {
			stmts, err := Statements(dialect, "authz_")
			must.NoError(t, err)

			for _, stmt := range stmts {
				test.StrNotContains(t, stmt, "--")
			}
		}
	})

	T.Run("rejects an unsupported dialect", func(t *testing.T) {
		t.Parallel()

		_, err := Statements("cockroach", "authz_")

		test.True(t, errors.Is(err, ErrUnsupportedDialect))
	})

	// The prefix is interpolated into DDL, so it is restricted rather than
	// escaped.
	T.Run("rejects prefixes that are not identifier fragments", func(t *testing.T) {
		t.Parallel()

		for _, prefix := range []string{
			`authz"; DROP TABLE users; --`,
			"authz-",
			"authz ",
			"1authz",
			"auth z",
		} {
			_, err := Statements(DialectSQLite, prefix)

			test.True(t, errors.Is(err, ErrInvalidTablePrefix))
		}
	})
}

func TestSQL(T *testing.T) {
	T.Parallel()

	T.Run("joins statements into one body", func(t *testing.T) {
		t.Parallel()

		for _, dialect := range allDialects() {
			ddl, err := SQL(dialect, "authz_")
			must.NoError(t, err)

			test.StrHasSuffix(t, ";\n", ddl)
			test.StrContains(t, ddl, "authz_roles")
		}
	})

	T.Run("propagates rendering errors", func(t *testing.T) {
		t.Parallel()

		_, err := SQL("cockroach", "authz_")
		test.True(t, errors.Is(err, ErrUnsupportedDialect))

		_, err = SQL(DialectSQLite, "bad-prefix")
		test.True(t, errors.Is(err, ErrInvalidTablePrefix))
	})
}
