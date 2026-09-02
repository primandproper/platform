package querygen

import (
	"context"
	"database/sql"
	"testing"

	"github.com/primandproper/platform-go/v14/database/dialect"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// The three statements a table with no id of its own needs to be written at
// all: the standalone insert, the insert that skips a row already there, and
// the hard delete.
//
// They are here rather than in the composite suite next door because they are
// Query forms rather than Bound ones — there is no Bound tier for any of them —
// and because two of the three promise something a string comparison cannot
// see. That an ignored insert reports no rows and leaves the row that was
// already there alone is a claim about three servers' duplicate handling; that
// a hard delete removes a row the archive had already stamped is a claim about
// a predicate that is absent. Both fail here or nowhere.
//
// The suite stands up its own table, like every other suite hanging off
// runDialect, so its writes cannot move what the others assert on.

const grantsTable = "grants"

// grantsDDL is the child-table shape this ticket is about: keyed on the parent
// it hangs off and the value it grants, with no id and no cursor.
//
// It carries archived_at, which a role table would not, because one of the
// assertions is that the hard delete reaches an archived row — and a column the
// table does not have cannot be left out of a predicate.
func grantsDDL(d dialect.Dialect) string {
	switch d {
	case dialect.MySQL:
		return `CREATE TABLE ` + grantsTable + ` (
			owner_id VARCHAR(64) NOT NULL,
			role VARCHAR(64) NOT NULL,
			archived_at DATETIME NULL,
			PRIMARY KEY (owner_id, role)
		)`
	case dialect.SQLite:
		return `CREATE TABLE ` + grantsTable + ` (
			owner_id TEXT NOT NULL,
			role TEXT NOT NULL,
			archived_at TEXT,
			PRIMARY KEY (owner_id, role)
		)`
	// Postgres, which For has already narrowed the alternatives to.
	default:
		return `CREATE TABLE ` + grantsTable + ` (
			owner_id TEXT NOT NULL,
			role TEXT NOT NULL,
			archived_at TIMESTAMP WITH TIME ZONE,
			PRIMARY KEY (owner_id, role)
		)`
	}
}

const grantOwnerColumn = "owner_id"

func grantsColumns() []string {
	return []string{grantOwnerColumn, "role", ArchivedAtColumn}
}

// grantsQueries is the set the suite runs: the two inserts, the clear keyed on
// the owner, and the archive the delete is asserted against.
func grantsQueries(d dialect.Dialect) []*Query {
	var (
		g     = For(d)
		key   = []Match{{Column: grantOwnerColumn}, {Column: "role"}}
		owner = Match{Column: grantOwnerColumn}
	)

	return []*Query{
		g.InsertQuery("InsertGrant", grantsTable, ForInsert(grantsColumns()), nil),
		g.InsertIgnoreQuery("InsertGrantIgnoringDuplicates", grantsTable, ForInsert(grantsColumns()), nil, key...),
		g.DeleteQuery("DeleteGrants", grantsTable, grantsColumns(), owner),
		g.ArchiveQuery("ArchiveGrant", grantsTable, grantsColumns(), key...),
	}
}

// execGrant runs one of them, binding the values the way sqlc's generated code
// would.
func execGrant(tb testing.TB, ctx context.Context, d dialect.Dialect, db *sql.DB, name string, values map[string]any) sql.Result {
	tb.Helper()

	statement, order := bindArguments(d, named(tb, grantsQueries(d), name).Content)

	result, err := db.ExecContext(ctx, statement, argumentsFor(tb, order, values)...)
	must.NoError(tb, err, must.Sprintf("executing %s:\n%s", name, statement))

	return result
}

// liveGrants reads back the pairs in the table, archived rows included, so an
// assertion can say which rows survived a write rather than only how many.
//
// The pair is joined in Go rather than by the server: string concatenation is
// the one thing here the three dialects genuinely spell three ways, and this
// read is the suite's own scaffolding rather than something the package emits.
func liveGrants(tb testing.TB, ctx context.Context, db *sql.DB) []string {
	tb.Helper()

	rows, err := db.QueryContext(ctx, "SELECT owner_id, role FROM "+grantsTable+" ORDER BY owner_id, role")
	must.NoError(tb, err)

	defer func() { must.NoError(tb, rows.Close()) }()

	var pairs []string

	for rows.Next() {
		var owner, role string

		must.NoError(tb, rows.Scan(&owner, &role))

		pairs = append(pairs, owner+"/"+role)
	}

	must.NoError(tb, rows.Err())

	return pairs
}

// runIDLessWriteSuite is written once and run against each of the three
// servers, like every other suite here.
func runIDLessWriteSuite(t *testing.T, ctx context.Context, d dialect.Dialect, db *sql.DB) {
	t.Helper()

	_, err := db.ExecContext(ctx, grantsDDL(d))
	must.NoError(t, err)

	grant := func(owner, role string) map[string]any {
		return map[string]any{grantOwnerColumn: owner, "role": role}
	}

	t.Run("every statement is one the server accepts", func(t *testing.T) {
		for _, query := range grantsQueries(d) {
			prepare(t, ctx, d, db, query)
		}
	})

	t.Run("the standalone insert writes a row for a table with no id", func(t *testing.T) {
		for _, role := range []string{"admin", "member"} {
			execGrant(t, ctx, d, db, "InsertGrant", grant("m_001", role))
		}

		execGrant(t, ctx, d, db, "InsertGrant", grant("m_002", "member"))

		test.Eq(t, []string{"m_001/admin", "m_001/member", "m_002/member"}, liveGrants(t, ctx, db))
	})

	t.Run("the ignoring insert reports the row it did not write", func(t *testing.T) {
		// The pair is already there, so the row that is there wins and the
		// count is how the caller learns it lost the race.
		result := execGrant(t, ctx, d, db, "InsertGrantIgnoringDuplicates", grant("m_001", "admin"))
		test.EqOp(t, int64(0), affectedRows(t, result))

		result = execGrant(t, ctx, d, db, "InsertGrantIgnoringDuplicates", grant("m_001", "auditor"))
		test.EqOp(t, int64(1), affectedRows(t, result))

		test.Eq(t,
			[]string{"m_001/admin", "m_001/auditor", "m_001/member", "m_002/member"},
			liveGrants(t, ctx, db))
	})

	t.Run("the plain insert raises on the duplicate the ignoring one skips", func(t *testing.T) {
		// Which is the whole difference between the two, and why the ignoring
		// form is a named shape rather than a flag.
		statement, order := bindArguments(d, named(t, grantsQueries(d), "InsertGrant").Content)

		_, insertErr := db.ExecContext(ctx, statement, argumentsFor(t, order, grant("m_001", "admin"))...)
		test.Error(t, insertErr)
	})

	t.Run("the hard delete removes an archived row", func(t *testing.T) {
		// The predicate that is not there. Every other statement keyed the same
		// way carries archived_at IS NULL, so an archived grant is invisible to
		// them — and a delete that could not reach it would be an erasure that
		// fails for exactly the subjects who were archived first.
		test.EqOp(t, int64(1), affectedRows(t, execGrant(t, ctx, d, db, "ArchiveGrant", grant("m_002", "member"))))

		result := execGrant(t, ctx, d, db, "DeleteGrants", map[string]any{grantOwnerColumn: "m_002"})
		test.EqOp(t, int64(1), affectedRows(t, result))

		test.Eq(t, []string{"m_001/admin", "m_001/auditor", "m_001/member"}, liveGrants(t, ctx, db))
	})

	t.Run("the delete clears the rows its key names and no others", func(t *testing.T) {
		// Keyed on the owner rather than on the whole primary key, which is
		// what makes it the clear a wholesale rewrite runs rather than the
		// removal of one row.
		execGrant(t, ctx, d, db, "InsertGrant", grant("m_003", "member"))

		result := execGrant(t, ctx, d, db, "DeleteGrants", map[string]any{grantOwnerColumn: "m_001"})
		test.EqOp(t, int64(3), affectedRows(t, result))

		test.Eq(t, []string{"m_003/member"}, liveGrants(t, ctx, db))

		// And a second clear finds nothing, which is how a caller learns the
		// owner had no grants rather than that the statement failed.
		result = execGrant(t, ctx, d, db, "DeleteGrants", map[string]any{grantOwnerColumn: "m_001"})
		test.EqOp(t, int64(0), affectedRows(t, result))
	})
}
