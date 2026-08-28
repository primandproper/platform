package querygen

import (
	"errors"
	"strings"
	"testing"

	"github.com/primandproper/platform-go/v13/database/dialect"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// subjectKeyColumns is the id-less table the insert-ignore was written for:
// one live key per (subject_type, subject_id), the pair being what a shred
// depends on.
var subjectKeyColumns = []string{"subject_type", "subject_id", "wrapped_key", CreatedAtColumn, "shredded_at"}

// subjectKeyTarget is the unique index a duplicate mint collides on.
var subjectKeyTarget = []Match{{Column: "subject_type"}, {Column: "subject_id"}}

func insertIgnoreFor(d dialect.Dialect) string {
	return For(d).InsertIgnoreQuery("InsertSubjectKey", "subject_keys",
		ForInsert(subjectKeyColumns), []string{"wrapped_key", "shredded_at"}, subjectKeyTarget...,
	).Content
}

func TestInsertQuery(T *testing.T) {
	T.Parallel()

	T.Run("annotates the statement as an exec", func(t *testing.T) {
		t.Parallel()

		query := For(dialect.Postgres).InsertQuery("InsertMembershipRole", "membership_roles", roleColumns, nil)

		test.EqOp(t, "InsertMembershipRole", query.Annotation.Name)
		test.EqOp(t, ExecType, query.Annotation.Type)
	})

	T.Run("writes a table StandardCRUD refuses", func(t *testing.T) {
		t.Parallel()

		// StandardCRUD needs an id because its list pages by keyset over one.
		// An INSERT needs no such thing, and this is the whole difference: the
		// same create, for the table whose key is (parent, value).
		must.Error(t, recovered(func() {
			For(dialect.Postgres).StandardCRUD("membership_roles", roleColumns)
		}))

		query := For(dialect.Postgres).InsertQuery("InsertMembershipRole", "membership_roles", roleColumns, nil)

		test.EqOp(t,
			"INSERT INTO membership_roles (\n\tmembership_id,\n\trole\n) VALUES (\n\tsqlc.arg(membership_id),\n\tsqlc.arg(role)\n);",
			query.Content)
	})

	T.Run("renders the create's own text", func(t *testing.T) {
		t.Parallel()

		// Same function, so a table's standard create and a standalone insert
		// over the same columns cannot come to disagree about the bindings.
		columns := ForInsert(widgetsColumns())

		for _, d := range everyDialect() {
			standard := named(t, For(d).StandardCRUD(widgetsTable, widgetsColumns(),
				WithEntity("Widget", "Widgets")), "CreateWidget")

			test.EqOp(t, standard.Content,
				For(d).InsertQuery("InsertWidget", widgetsTable, columns, nil).Content)
		}
	})

	T.Run("binds a nullable column through narg", func(t *testing.T) {
		t.Parallel()

		query := For(dialect.Postgres).InsertQuery("InsertNote", "notes",
			[]string{"subject", "note"}, []string{"note"})

		test.StrContains(t, query.Content, "sqlc.arg(subject)")
		test.StrContains(t, query.Content, "sqlc.narg(note)")
	})

	T.Run("takes one argument per column", func(t *testing.T) {
		t.Parallel()

		for _, d := range everyDialect() {
			_, args := bindArguments(d, For(d).InsertQuery("InsertMembershipRole",
				"membership_roles", roleColumns, nil).Content)

			test.Eq(t, roleColumns, args)
		}
	})

	T.Run("refuses an insert with no columns", func(t *testing.T) {
		t.Parallel()

		// An INSERT with an empty column list is a syntax error rather than a
		// degenerate write, so it is refused here rather than by a server.
		err := recovered(func() {
			For(dialect.Postgres).InsertQuery("InsertNothing", "membership_roles", nil, nil)
		})

		must.Error(t, err)
		test.True(t, errors.Is(err, ErrDegenerateInsert))
		test.StrContains(t, err.Error(), "inserts no columns")
	})

	T.Run("renders one text on all three dialects", func(t *testing.T) {
		t.Parallel()

		rendered := map[dialect.Dialect]string{}
		for _, d := range everyDialect() {
			rendered[d] = For(d).InsertQuery("InsertMembershipRole", "membership_roles", roleColumns, nil).Content
		}

		test.EqOp(t, rendered[dialect.Postgres], rendered[dialect.MySQL])
		test.EqOp(t, rendered[dialect.Postgres], rendered[dialect.SQLite])
	})
}

func TestInsertIgnoreQuery(T *testing.T) {
	T.Parallel()

	T.Run("annotates the statement as an execrows", func(t *testing.T) {
		t.Parallel()

		// The count is the whole point: the loser of a race between two mints
		// has generated a key it has to throw away.
		query := For(dialect.Postgres).InsertIgnoreQuery("InsertSubjectKey", "subject_keys",
			ForInsert(subjectKeyColumns), nil, subjectKeyTarget...)

		test.EqOp(t, "InsertSubjectKey", query.Annotation.Name)
		test.EqOp(t, ExecRowsType, query.Annotation.Type)
	})

	T.Run("inserts what the plain insert inserts", func(t *testing.T) {
		t.Parallel()

		// The INSERT half is the create's rendering with a modifier in front of
		// INTO, so the ignoring form and the plain one cannot disagree about
		// which columns a caller supplies.
		plain := createStatement("subject_keys", ForInsert(subjectKeyColumns), []string{"wrapped_key", "shredded_at"})

		for _, d := range everyDialect() {
			modifier, clause := For(d).ignoreSpelling([]string{"subject_type", "subject_id"})

			statement := insertIgnoreFor(d)
			if clause != "" {
				statement = strings.TrimSuffix(statement, "\n"+clause+";") + ";"
			}

			test.EqOp(t, plain, strings.Replace(statement, "INSERT "+modifier, "INSERT ", 1))
		}
	})

	T.Run("names the conflict target on Postgres", func(t *testing.T) {
		t.Parallel()

		statement := insertIgnoreFor(dialect.Postgres)

		test.StrContains(t, statement, "ON CONFLICT (subject_type, subject_id) DO NOTHING;")
		test.StrContains(t, statement, "INSERT INTO subject_keys")
	})

	T.Run("renders MySQL's and SQLite's targetless modifiers", func(t *testing.T) {
		t.Parallel()

		mysqlStatement := insertIgnoreFor(dialect.MySQL)

		test.StrContains(t, mysqlStatement, "INSERT IGNORE INTO subject_keys")
		test.StrNotContains(t, mysqlStatement, "ON CONFLICT")

		sqliteStatement := insertIgnoreFor(dialect.SQLite)

		test.StrContains(t, sqliteStatement, "INSERT OR IGNORE INTO subject_keys")
		test.StrNotContains(t, sqliteStatement, "ON CONFLICT")
	})

	T.Run("binds the columns and not the target", func(t *testing.T) {
		t.Parallel()

		// An INSERT has no WHERE, so the target names an index rather than
		// adding a predicate — the arguments are the create's exactly.
		for _, d := range everyDialect() {
			_, args := bindArguments(d, insertIgnoreFor(d))

			test.Eq(t, ForInsert(subjectKeyColumns), args)
		}
	})

	T.Run("assigns nothing to the row it found", func(t *testing.T) {
		t.Parallel()

		// The distinction from the upsert, which ErrDegenerateUpsert refuses to
		// render without an assignment: here the row already there wins,
		// unchanged.
		for _, d := range everyDialect() {
			test.StrNotContains(t, insertIgnoreFor(d), "DO UPDATE")
			test.StrNotContains(t, insertIgnoreFor(d), "ON DUPLICATE KEY UPDATE")
		}
	})

	T.Run("refuses an insert-ignore with no conflict target", func(t *testing.T) {
		t.Parallel()

		err := recovered(func() {
			For(dialect.Postgres).InsertIgnoreQuery("InsertSubjectKey", "subject_keys",
				ForInsert(subjectKeyColumns), nil)
		})

		must.Error(t, err)
		test.True(t, errors.Is(err, ErrDegenerateInsert))
		test.StrContains(t, err.Error(), "names no conflict target")
	})

	T.Run("refuses an insert-ignore with no columns", func(t *testing.T) {
		t.Parallel()

		err := recovered(func() {
			For(dialect.Postgres).InsertIgnoreQuery("InsertSubjectKey", "subject_keys", nil, nil, subjectKeyTarget...)
		})

		must.Error(t, err)
		test.True(t, errors.Is(err, ErrDegenerateInsert))
		test.StrContains(t, err.Error(), "inserts no columns")
	})
}

func TestInsertIgnoreQuery_RendersOneNameThreeTexts(t *testing.T) {
	t.Parallel()

	// One call, one query name, three statements whose difference is the
	// duplicate-skipping grammar and nothing else. Unlike the upsert, all three
	// differ: MySQL and SQLite take different modifiers.
	rendered := map[dialect.Dialect]string{}

	for _, d := range everyDialect() {
		query := For(d).InsertIgnoreQuery("InsertSubjectKey", "subject_keys",
			ForInsert(subjectKeyColumns), nil, subjectKeyTarget...)

		test.EqOp(t, "InsertSubjectKey", query.Annotation.Name)

		rendered[d] = query.Content
	}

	test.NotEqOp(t, rendered[dialect.Postgres], rendered[dialect.MySQL])
	test.NotEqOp(t, rendered[dialect.Postgres], rendered[dialect.SQLite])
	test.NotEqOp(t, rendered[dialect.MySQL], rendered[dialect.SQLite])
}
