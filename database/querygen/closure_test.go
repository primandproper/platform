package querygen

import (
	"strings"
	"testing"

	"github.com/primandproper/platform-go/v13/database/dialect"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// The three tables a closure read spans, at the shape authorization's policy
// schema has: a conventional table the walk moves through, a mapping table
// whose two columns are both references into it, and a second mapping table
// reaching a second conventional table.
const (
	rolesTable          = "roles"
	permissionsTable    = "permissions"
	roleParentsTable    = "role_parents"
	rolePermissionTable = "role_permissions"
)

func namedColumns() []string {
	return []string{IDColumn, "name", "description", CreatedAtColumn, LastUpdatedAtColumn, ArchivedAtColumn}
}

// roleClosure is the walk every test below renders, spelled once so a test
// changing one field says what it is changing.
func roleClosure() *Closure {
	return &Closure{
		Alias:      "role_closure",
		Walk:       Edge{Table: roleParentsTable, From: "child_role_id", To: "parent_role_id"},
		Reach:      Edge{Table: rolePermissionTable, From: "role_id", To: "permission_id"},
		Table:      permissionsTable,
		Columns:    namedColumns(),
		Projection: []string{"name"},
	}
}

func closureQuery(d dialect.Dialect, closure *Closure) *Query {
	return For(d).ClosureQuery("ResolvePermissionsForRoles", rolesTable, namedColumns(),
		closure, SetKey{Column: "name", Arg: "names"})
}

func TestGenerator_ClosureQuery(T *testing.T) {
	T.Parallel()

	T.Run("names the query and answers with many rows", func(t *testing.T) {
		t.Parallel()

		for _, d := range everyDialect() {
			q := closureQuery(d, roleClosure())

			test.EqOp(t, "ResolvePermissionsForRoles", q.Annotation.Name)

			// :many, because a closure is a set: the whole point is that the
			// depth is data rather than a number the caller knows.
			test.EqOp(t, ManyType, q.Annotation.Type, test.Sprintf("dialect %q", d))
		}
	})

	// UNION ALL is the faster spelling and it is the one that never returns, on
	// the query that decides whether a request is allowed. It is the shape's
	// choice rather than the caller's, so there is no argument that relaxes it.
	T.Run("terminates on a cycle", func(t *testing.T) {
		t.Parallel()

		for _, d := range everyDialect() {
			q := closureQuery(d, roleClosure())

			test.StrContains(t, q.Content, "WITH RECURSIVE role_closure AS", test.Sprintf("dialect %q", d))
			test.StrContains(t, q.Content, "\n\tUNION\n", test.Sprintf("dialect %q", d))
			test.StrNotContains(t, q.Content, "UNION ALL", test.Sprintf("dialect %q", d))
		}
	})

	// Excluding archived rows only at the seed is the comfortable mistake: the
	// statement still looks keyed, still returns rows, and still passes a test
	// that archives the row it asks about — while going on reaching through an
	// archived intermediary.
	T.Run("excludes archived rows at every join", func(t *testing.T) {
		t.Parallel()

		for _, d := range everyDialect() {
			q := closureQuery(d, roleClosure())

			test.EqOp(t, 2, strings.Count(q.Content, Qualify(rolesTable, ArchivedAtColumn)+" IS NULL"),
				test.Sprintf("dialect %q", d))
			test.EqOp(t, 1, strings.Count(q.Content, Qualify(permissionsTable, ArchivedAtColumn)+" IS NULL"),
				test.Sprintf("dialect %q", d))
		}
	})

	// A table that does not soft-delete gets no predicate at all rather than an
	// empty WHERE, which is a syntax error on every dialect. The recursive term
	// is the one that would render it, since the joins are the whole of what it
	// otherwise says.
	T.Run("a table with no archived_at renders no liveness clause", func(t *testing.T) {
		t.Parallel()

		closure := roleClosure()
		closure.Columns = []string{IDColumn, "name"}

		q := For(dialect.Postgres).ClosureQuery("ResolveThings", rolesTable,
			[]string{IDColumn, "name"}, closure, SetKey{Column: "name"})

		test.StrNotContains(t, q.Content, ArchivedAtColumn)
		test.StrNotContains(t, q.Content, "WHERE\n")
		test.StrNotContains(t, q.Content, "WHERE)")
	})

	T.Run("walks the edge in the direction it was given", func(t *testing.T) {
		t.Parallel()

		for _, d := range everyDialect() {
			q := closureQuery(d, roleClosure())

			// The edge's From matches a row already reached, and its To names
			// the row the walk adds. Reversed, the same statement answers the
			// opposite question.
			test.StrContains(t, q.Content,
				"JOIN "+roleParentsTable+" ON role_closure."+IDColumn+"="+Qualify(roleParentsTable, "child_role_id"),
				test.Sprintf("dialect %q", d))
			test.StrContains(t, q.Content,
				"JOIN "+rolesTable+" ON "+Qualify(roleParentsTable, "parent_role_id")+"="+Qualify(rolesTable, IDColumn),
				test.Sprintf("dialect %q", d))
		}
	})

	T.Run("reads through the second edge into the projected table", func(t *testing.T) {
		t.Parallel()

		for _, d := range everyDialect() {
			q := closureQuery(d, roleClosure())

			test.StrContains(t, q.Content,
				"JOIN "+rolePermissionTable+" ON role_closure."+IDColumn+"="+Qualify(rolePermissionTable, "role_id"),
				test.Sprintf("dialect %q", d))
			test.StrContains(t, q.Content,
				"JOIN "+permissionsTable+" ON "+Qualify(rolePermissionTable, "permission_id")+"="+Qualify(permissionsTable, IDColumn),
				test.Sprintf("dialect %q", d))
		}
	})

	// Two roles granting one permission is the ordinary case rather than an
	// anomaly, so a duplicate is the walk's arithmetic showing through; and a
	// set whose order is whichever the planner found convenient is a set two
	// identical calls can answer differently.
	T.Run("answers a deduplicated, ordered set", func(t *testing.T) {
		t.Parallel()

		for _, d := range everyDialect() {
			q := closureQuery(d, roleClosure())

			test.StrContains(t, q.Content, "SELECT DISTINCT\n\t"+Qualify(permissionsTable, "name"),
				test.Sprintf("dialect %q", d))

			// Exactly the projected columns, which is what a SELECT DISTINCT is
			// allowed to order by on all three servers.
			test.True(t, strings.HasSuffix(q.Content, "ORDER BY "+Qualify(permissionsTable, "name")+";"),
				test.Sprintf("dialect %q", d))
		}
	})

	T.Run("seeds from the bound set in the dialect's own spelling", func(t *testing.T) {
		t.Parallel()

		for _, d := range everyDialect() {
			g := For(d)
			q := closureQuery(d, roleClosure())

			test.StrContains(t, q.Content, g.setPredicate(Qualify(rolesTable, "name"), "names"),
				test.Sprintf("dialect %q", d))
		}
	})

	// On the two dialects with no array type the set expands into one bare
	// placeholder per element, and an argument bound after it collides with an
	// element of the set.
	T.Run("renders the set after every other seed argument", func(t *testing.T) {
		t.Parallel()

		for _, d := range everyDialect() {
			q := For(d).ClosureQuery("ResolveScopedPermissionsForRoles", rolesTable, namedColumns(),
				roleClosure(), SetKey{Column: "name", Arg: "names"},
				Match{Column: "scope"})

			set := setArgument.FindStringIndex(q.Content)
			must.NotNil(t, set, must.Sprintf("dialect %q renders no set", d))

			test.False(t, strings.Contains(q.Content[set[1]:], "sqlc."),
				test.Sprintf("dialect %q binds an argument after the set", d))
		}
	})

	T.Run("narrows the seed by the matches it is given", func(t *testing.T) {
		t.Parallel()

		q := For(dialect.Postgres).ClosureQuery("ResolveScopedPermissionsForRoles", rolesTable,
			namedColumns(), roleClosure(), SetKey{Column: "name", Arg: "names"},
			Match{Column: "scope"})

		// In the seed alone: the recursive term is about rows the walk reached,
		// not about which rows a caller asked to start from.
		test.EqOp(t, 1, strings.Count(q.Content, Qualify(rolesTable, "scope")+" = sqlc.arg(scope)"))
	})

	// The key is a field rather than a constant because a walked table need not
	// be keyed on an id, and the tables that are not are the ones this shape is
	// most likely to meet next.
	T.Run("keys on the column the closure names", func(t *testing.T) {
		t.Parallel()

		closure := roleClosure()
		closure.Key = "role_key"
		closure.TableKey = "permission_key"

		q := closureQuery(dialect.Postgres, closure)

		test.StrContains(t, q.Content, "SELECT\n\t\t"+Qualify(rolesTable, "role_key"))
		test.StrContains(t, q.Content, "ON role_closure.role_key="+Qualify(roleParentsTable, "child_role_id"))
		test.StrContains(t, q.Content,
			Qualify(rolePermissionTable, "permission_id")+"="+Qualify(permissionsTable, "permission_key"))
	})
}

func TestGenerator_ClosureQuery_Refusals(T *testing.T) {
	T.Parallel()

	// Every one of these renders SQL a server rejects rather than SQL that
	// quietly answers the wrong question. They are refused here anyway, because
	// the message says which half is missing where a parse error says a token
	// was unexpected eleven lines into a statement nobody wrote by hand.
	for name, closure := range map[string]*Closure{
		"no alias":              withClosure(func(c *Closure) { c.Alias = "" }),
		"no table to read":      withClosure(func(c *Closure) { c.Table = "" }),
		"nothing projected":     withClosure(func(c *Closure) { c.Projection = nil }),
		"walk names no table":   withClosure(func(c *Closure) { c.Walk.Table = "" }),
		"walk names no columns": withClosure(func(c *Closure) { c.Walk.To = "" }),
		"reach names no table":  withClosure(func(c *Closure) { c.Reach.Table = "" }),
		"reach names no column": withClosure(func(c *Closure) { c.Reach.From = "" }),
	} {
		T.Run(name, func(t *testing.T) {
			t.Parallel()

			test.ErrorIs(t, closurePanic(t, closure, SetKey{Column: "name"}), ErrIncompleteClosure)
		})
	}

	// A recursive read with no recursion in it is the plain batched read this
	// package already emits, so a nil closure is a call that meant a different
	// function rather than a statement with a part missing.
	T.Run("no closure at all", func(t *testing.T) {
		t.Parallel()

		test.ErrorIs(t, closurePanic(t, nil, SetKey{Column: "name"}), ErrIncompleteClosure)
	})

	T.Run("a set keyed on no column", func(t *testing.T) {
		t.Parallel()

		test.ErrorIs(t, closurePanic(t, roleClosure(), SetKey{}), ErrMissingSetColumn)
	})

	// Every identifier here is interpolated rather than bound, so it is
	// restricted rather than escaped.
	for name, closure := range map[string]*Closure{
		"an unsafe alias":      withClosure(func(c *Closure) { c.Alias = "role\"; DROP TABLE roles; --" }),
		"an unsafe walk table": withClosure(func(c *Closure) { c.Walk.Table = "parents; DROP TABLE roles" }),
		"an unsafe projection": withClosure(func(c *Closure) { c.Projection = []string{"name; --"} }),
	} {
		T.Run(name, func(t *testing.T) {
			t.Parallel()

			test.ErrorIs(t, closurePanic(t, closure, SetKey{Column: "name"}), dialect.ErrInvalidIdentifier)
		})
	}
}

// withClosure returns roleClosure with one field broken.
func withClosure(damage func(*Closure)) *Closure {
	closure := roleClosure()
	damage(closure)

	return closure
}

// closurePanic renders the query and returns whatever it panicked with, which
// is how this package reports misuse — its arguments are string literals in a
// generator binary, so every way it fails is a typo a build should stop for.
func closurePanic(t *testing.T, closure *Closure, key SetKey) error {
	t.Helper()

	var err error

	func() {
		defer func() {
			value := recover()
			must.NotNil(t, value, must.Sprintf("rendered without panicking"))

			panicked, ok := value.(error)
			must.True(t, ok, must.Sprintf("panicked with %T rather than an error", value))

			err = panicked
		}()

		_ = For(dialect.Postgres).ClosureQuery("Resolve", rolesTable, namedColumns(), closure, key)
	}()

	return err
}
