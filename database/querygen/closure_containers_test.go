package querygen

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/primandproper/platform-go/v14/database/dialect"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// The closure read is the one shape here whose correctness a string comparison
// cannot reach. Both of its properties are behavioral: that a hierarchy edited
// into a cycle terminates the walk rather than hanging it, and that an archived
// row stops the walk at itself rather than merely failing to seed it. Each is a
// statement about what a server does with the SQL, and each is exactly the
// failure that survives a test written against a policy with no cycle and
// nothing archived.
//
// The suite stands up its own four tables, like every other suite hanging off
// runDialect, so its writes cannot move what the others assert on.
const (
	closureRolesTable       = "closure_roles"
	closurePermissionsTable = "closure_permissions"
	closureParentsTable     = "closure_role_parents"
	closureGrantsTable      = "closure_role_permissions"
)

// closureDDL is a conventional table twice over and two mapping tables between
// them, which is the schema shape this read is written for: the named rows
// carry archived_at and the edges deliberately do not.
func closureDDL(d dialect.Dialect) []string {
	named := func(table string) string {
		switch d {
		case dialect.MySQL:
			return fmt.Sprintf(`CREATE TABLE %s (
				id VARCHAR(64) NOT NULL PRIMARY KEY,
				name VARCHAR(191) NOT NULL,
				description VARCHAR(255) NOT NULL,
				created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
				last_updated_at DATETIME NULL,
				archived_at DATETIME NULL,
				UNIQUE KEY %s_name_idx (name)
			)`, table, table)
		case dialect.SQLite:
			return fmt.Sprintf(`CREATE TABLE %s (
				id TEXT NOT NULL PRIMARY KEY,
				name TEXT NOT NULL UNIQUE,
				description TEXT NOT NULL,
				created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
				last_updated_at TEXT,
				archived_at TEXT
			)`, table)
		// Postgres, which For has already narrowed the alternatives to.
		default:
			return fmt.Sprintf(`CREATE TABLE %s (
				id TEXT NOT NULL PRIMARY KEY,
				name TEXT NOT NULL UNIQUE,
				description TEXT NOT NULL,
				created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
				last_updated_at TIMESTAMP WITH TIME ZONE,
				archived_at TIMESTAMP WITH TIME ZONE
			)`, table)
		}
	}

	// The edge tables carry no foreign keys, which is this suite's choice
	// rather than the shape's: the cycle case below writes a hierarchy no
	// well-behaved store would, and a schema that refused it would refuse the
	// test that proves the query survives it.
	edge := func(table, from, to string) string {
		width := "TEXT"
		if d == dialect.MySQL {
			width = "VARCHAR(64)"
		}

		return fmt.Sprintf(`CREATE TABLE %s (
			%s %s NOT NULL,
			%s %s NOT NULL,
			PRIMARY KEY (%[2]s, %[4]s)
		)`, table, from, width, to, width)
	}

	return []string{
		named(closureRolesTable),
		named(closurePermissionsTable),
		edge(closureParentsTable, "child_role_id", "parent_role_id"),
		edge(closureGrantsTable, "role_id", "permission_id"),
	}
}

// closureArchivalTime is when the archived rows in this suite were archived. It
// is a fixed instant in the past rather than time.Now, because what the
// statement asks of the column is whether it is NULL and nothing else — so a
// value that moves between runs would be a difference nothing reads.
func closureArchivalTime() time.Time {
	return time.Date(2020, time.January, 1, 0, 0, 0, 0, time.UTC)
}

// closureResolve is the statement under test, at the shape authorization's
// policy resolution renders.
func closureResolve(d dialect.Dialect) *Query {
	return For(d).ClosureQuery("ResolveClosurePermissions", closureRolesTable, namedColumns(),
		&Closure{
			Alias:      "closure",
			Walk:       Edge{Table: closureParentsTable, From: "child_role_id", To: "parent_role_id"},
			Reach:      Edge{Table: closureGrantsTable, From: "role_id", To: "permission_id"},
			Table:      closurePermissionsTable,
			Columns:    namedColumns(),
			Projection: []string{"name"},
		},
		SetKey{Column: "name", Arg: "names"})
}

// resolveClosure runs the read for a set of seed names and returns what it
// answered, in the order the statement promised.
func resolveClosure(tb testing.TB, ctx context.Context, d dialect.Dialect, db *sql.DB, names ...string) []string {
	tb.Helper()

	rewritten, expanded := expandSlices(tb, closureResolve(d).Content, map[string]any{"names": names})
	statement, order := bindArguments(d, rewritten)

	rows, err := db.QueryContext(ctx, statement, argumentsFor(tb, order, expanded)...)
	must.NoError(tb, err)

	defer func() { must.NoError(tb, rows.Close()) }()

	var found []string

	for rows.Next() {
		var name string
		must.NoError(tb, rows.Scan(&name))
		found = append(found, name)
	}

	must.NoError(tb, rows.Err())

	return found
}

// closureFixture is one named row this suite writes: roles and permissions are
// the same table twice, so they are the same fixture twice.
type closureFixture struct {
	id, name string
	archived bool
}

// insertClosureRow writes a named row, archived or not.
func insertClosureRow(tb testing.TB, ctx context.Context, d dialect.Dialect, db *sql.DB, table, id, name string, archived bool) {
	tb.Helper()

	var archivedAt any
	if archived {
		archivedAt = timeArg(d, closureArchivalTime())
	}

	_, err := db.ExecContext(ctx, fmt.Sprintf(
		"INSERT INTO %s (id, name, description, archived_at) VALUES (%s, %s, %s, %s)",
		table, d.Placeholder(1), d.Placeholder(2), d.Placeholder(3), d.Placeholder(4),
	), id, name, "", archivedAt)
	must.NoError(tb, err)
}

// insertClosureEdge writes one mapping row.
func insertClosureEdge(tb testing.TB, ctx context.Context, d dialect.Dialect, db *sql.DB, table, from, to string) {
	tb.Helper()

	_, err := db.ExecContext(ctx, fmt.Sprintf("INSERT INTO %s VALUES (%s, %s)",
		table, d.Placeholder(1), d.Placeholder(2)), from, to)
	must.NoError(tb, err)
}

// runClosureSuite is the whole of it: a hierarchy four deep, an archived
// intermediary, a permission archived out from under a live role, and a cycle.
func runClosureSuite(t *testing.T, ctx context.Context, d dialect.Dialect, db *sql.DB) {
	t.Helper()

	for _, statement := range closureDDL(d) {
		_, err := db.ExecContext(ctx, statement)
		must.NoError(t, err)
	}

	// member <- admin <- owner, plus a fourth role reached only through a role
	// that will be archived, and a fifth pair that closes a cycle.
	roles := []closureFixture{
		{id: "r_member", name: "member"},
		{id: "r_admin", name: "admin"},
		{id: "r_owner", name: "owner"},
		{id: "r_retired", name: "retired", archived: true},
		{id: "r_ancient", name: "ancient"},
		{id: "r_loop_a", name: "loop_a"},
		{id: "r_loop_b", name: "loop_b"},
	}

	for i := range roles {
		row := &roles[i]
		insertClosureRow(t, ctx, d, db, closureRolesTable, row.id, row.name, row.archived)
	}

	permissions := []closureFixture{
		{id: "p_read", name: "read"},
		{id: "p_write", name: "write"},
		{id: "p_delete", name: "delete"},
		{id: "p_ancient", name: "ancient_thing"},
		{id: "p_revoked", name: "revoked", archived: true},
		{id: "p_loop", name: "loop_thing"},
	}

	for i := range permissions {
		row := &permissions[i]
		insertClosureRow(t, ctx, d, db, closurePermissionsTable, row.id, row.name, row.archived)
	}

	// The hierarchy: admin inherits member, owner inherits admin and the
	// archived retired role, retired inherits ancient, and the last pair is the
	// cycle.
	parents := [][2]string{
		{"r_admin", "r_member"},
		{"r_owner", "r_admin"},
		{"r_owner", "r_retired"},
		{"r_retired", "r_ancient"},
		{"r_loop_a", "r_loop_b"},
		{"r_loop_b", "r_loop_a"},
	}

	for i := range parents {
		insertClosureEdge(t, ctx, d, db, closureParentsTable, parents[i][0], parents[i][1])
	}

	grants := [][2]string{
		{"r_member", "p_read"},
		{"r_admin", "p_write"},
		{"r_admin", "p_revoked"},
		{"r_owner", "p_delete"},
		{"r_ancient", "p_ancient"},
		{"r_loop_a", "p_loop"},
	}

	for i := range grants {
		insertClosureEdge(t, ctx, d, db, closureGrantsTable, grants[i][0], grants[i][1])
	}

	t.Run("expands inheritance to whatever depth the data has", func(t *testing.T) {
		// owner reaches admin reaches member, which is two levels the seed
		// knows nothing about — and the answer is ordered and deduplicated.
		test.Eq(t, []string{"delete", "read", "write"}, resolveClosure(t, ctx, d, db, "owner"))
	})

	t.Run("a permission granted by two roles comes back once", func(t *testing.T) {
		test.Eq(t, []string{"read", "write"}, resolveClosure(t, ctx, d, db, "admin", "member"))
	})

	// The archived predicate in the recursive term is what does this. Excluded
	// only at the seed, owner would still reach ancient_thing through the
	// retired role, which is the failure a test with nothing archived cannot
	// see.
	t.Run("an archived role stops the walk at itself", func(t *testing.T) {
		test.SliceNotContains(t, resolveClosure(t, ctx, d, db, "owner"), "ancient_thing")
	})

	t.Run("an archived role is not a seed either", func(t *testing.T) {
		test.SliceEmpty(t, resolveClosure(t, ctx, d, db, "retired"))
	})

	// Archiving a permission revokes it everywhere on the next resolution,
	// without touching a mapping row.
	t.Run("an archived permission is revoked wherever it was granted", func(t *testing.T) {
		test.SliceNotContains(t, resolveClosure(t, ctx, d, db, "admin"), "revoked")
	})

	// UNION rather than UNION ALL. Under UNION ALL this call does not return,
	// which is the failure the shape refuses to be able to render.
	t.Run("a cycle terminates rather than hanging", func(t *testing.T) {
		test.Eq(t, []string{"loop_thing"}, resolveClosure(t, ctx, d, db, "loop_a"))
	})

	t.Run("a name nothing matches resolves to nothing", func(t *testing.T) {
		test.SliceEmpty(t, resolveClosure(t, ctx, d, db, "ghost"))
	})
}
