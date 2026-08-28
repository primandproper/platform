package querygen

import (
	"testing"

	"github.com/primandproper/platform-go/v13/database/dialect"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// The fixture for a table whose primary key is natural rather than a surrogate
// id — shredding_subject_keys' shape, whose key is what enforces one live key
// per subject. It has every column this package has an opinion about except the
// one StandardCRUD insists on.
const compositeTable = "sprockets"

func compositeColumns() []string {
	return []string{
		"subject_type",
		"subject_id",
		"key_material",
		CreatedAtColumn,
		LastUpdatedAtColumn,
		ArchivedAtColumn,
	}
}

// compositeKey is what such a table's statements pass where a conventional one
// passes nothing and lets the id predicate appear on its own.
func compositeKey() []Match {
	return []Match{{Column: "subject_type"}, {Column: "subject_id"}}
}

// compositeQueries is the natural-key counterpart of keyedQueries: the four
// single-row statements, each keyed on the whole natural key, and the insert
// that keys on nothing.
func compositeQueries(d dialect.Dialect) map[string]*Query {
	var (
		g       = For(d)
		columns = compositeColumns()
		key     = compositeKey()
	)

	return map[string]*Query{
		"create":  g.CreateQuery("CreateSprocket", compositeTable, ForInsert(columns), nil),
		"get":     g.GetQuery("GetSprocket", compositeTable, columns, key...),
		"exists":  g.ExistsQuery("CheckSprocketExistence", compositeTable, columns, key...),
		"update":  g.UpdateQuery("UpdateSprocket", compositeTable, columns, ForUpdate(columns, "subject_type", "subject_id"), nil, key...),
		"archive": g.ArchiveQuery("ArchiveSprocket", compositeTable, columns, key...),
	}
}

// singleRow is compositeQueries less the insert, which keys on nothing and so
// says nothing about a key.
func singleRow(queries map[string]*Query) map[string]*Query {
	out := make(map[string]*Query, len(queries)-1)

	for name, q := range queries {
		if name != "create" {
			out[name] = q
		}
	}

	return out
}

func TestNaturalKeyQueries(T *testing.T) {
	T.Parallel()

	T.Run("address a row by the whole natural key", func(t *testing.T) {
		t.Parallel()

		// The id predicate is presence-conditional like every other predicate
		// here, so a table that deliberately has no id gets a statement keyed
		// on what it does key on rather than one naming a column the schema
		// does not have.
		for _, d := range everyDialect() {
			for name, q := range singleRow(compositeQueries(d)) {
				sql, args := bindQuery(d, q)

				test.SliceContains(t, args, "subject_type", test.Sprintf("dialect %q, statement %q", d, name))
				test.SliceContains(t, args, "subject_id", test.Sprintf("dialect %q, statement %q", d, name))
				test.SliceNotContains(t, args, IDColumn, test.Sprintf("dialect %q, statement %q", d, name))
				test.StrNotContains(t, sql, Qualify(compositeTable, IDColumn),
					test.Sprintf("dialect %q, statement %q", d, name))
				test.StrNotContains(t, sql, "sqlc.", test.Sprintf("dialect %q, statement %q", d, name))
				assertMarkersMatchArgs(t, d, sql, args)
			}
		}
	})

	T.Run("still exclude archived rows outright", func(t *testing.T) {
		t.Parallel()

		// Losing this alongside the id predicate would be the quiet half of the
		// change: a get that reads shredded rows back, and an archive that
		// restamps one already archived and reports it as a write.
		for _, d := range everyDialect() {
			queries := compositeQueries(d)

			test.StrContains(t, queries["get"].Content, Qualify(compositeTable, ArchivedAtColumn)+" IS NULL",
				test.Sprintf("dialect %q", d))
			test.StrContains(t, queries["archive"].Content, ArchivedAtColumn+" IS NULL", test.Sprintf("dialect %q", d))
			test.StrContains(t, queries["update"].Content, ArchivedAtColumn+" IS NULL", test.Sprintf("dialect %q", d))
		}
	})

	T.Run("exists selects something every server accepts rather than a column the table lacks", func(t *testing.T) {
		t.Parallel()

		// Nothing reads the EXISTS subquery's projection, so the id case keeps
		// selecting the id — the emitted SQL of every conventional table in the
		// module is unchanged — and the no-id case selects the literal.
		for _, d := range everyDialect() {
			test.StrContains(t, compositeQueries(d)["exists"].Content, "SELECT 1", test.Sprintf("dialect %q", d))
			test.StrContains(t, For(d).ExistsQuery("CheckGadgetExistence", keyedTable, keyedColumns(), keyedMatch()).Content,
				"SELECT "+Qualify(keyedTable, IDColumn), test.Sprintf("dialect %q", d))
		}
	})

	T.Run("exists asks what get asks", func(t *testing.T) {
		t.Parallel()

		for _, d := range everyDialect() {
			queries := compositeQueries(d)

			_, get := bindQuery(d, queries["get"])
			_, exists := bindQuery(d, queries["exists"])

			test.Eq(t, get, exists, test.Sprintf("dialect %q", d))
		}
	})

	T.Run("archive keys the row on exactly what the other three key it on", func(t *testing.T) {
		t.Parallel()

		// archiveStatement used to build its own predicate list, which is how
		// it came to be the one statement here that could not skip the id. It
		// routes through singleRowPredicates now, so there is one rendering of
		// "this row, unarchived" rather than two.
		for _, d := range everyDialect() {
			queries := compositeQueries(d)

			_, get := bindQuery(d, queries["get"])
			_, archive := bindQuery(d, queries["archive"])

			test.Eq(t, get, archive, test.Sprintf("dialect %q", d))
		}

		// And on a conventional table it still keys on the id, in the position
		// it always did.
		_, args := bindQuery(dialect.Postgres, For(dialect.Postgres).ArchiveQuery("ArchiveGadget",
			keyedTable, keyedColumns(), keyedMatch()))

		test.Eq(t, []string{IDColumn, BelongsToAccountColumn}, args)
	})

	T.Run("the archived predicate follows the column list like the id one", func(t *testing.T) {
		t.Parallel()

		// The parameter ArchiveQuery takes is the table's columns, so a caller
		// handing it a subset gets a statement derived from that subset. It is
		// the reason the doc says to pass the table's columns rather than the
		// ones this call happens to touch.
		for _, d := range everyDialect() {
			q := For(d).ArchiveQuery("ArchiveSprocket", compositeTable,
				without(compositeColumns(), ArchivedAtColumn), compositeKey()...)

			test.StrNotContains(t, q.Content, "IS NULL", test.Sprintf("dialect %q", d))
		}
	})
}

func TestSingleRowQueriesKeyOnSomething(T *testing.T) {
	T.Parallel()

	T.Run("a single-row statement with no id and no match is refused", func(t *testing.T) {
		t.Parallel()

		// Rather than a WHERE clause that is the archived predicate and nothing
		// else: a get returning the whole table, an update assigning every row
		// in it, and an archive emptying it.
		columns := without(compositeColumns(), IDColumn)

		for _, d := range everyDialect() {
			g := For(d)

			statements := map[string]func(){
				"get":     func() { _ = g.GetQuery("GetSprocket", compositeTable, columns) },
				"exists":  func() { _ = g.ExistsQuery("CheckSprocketExistence", compositeTable, columns) },
				"update":  func() { _ = g.UpdateQuery("UpdateSprocket", compositeTable, columns, ForUpdate(columns), nil) },
				"archive": func() { _ = g.ArchiveQuery("ArchiveSprocket", compositeTable, columns) },
			}

			for name, render := range statements {
				err := recovered(render)

				must.Error(t, err, must.Sprintf("dialect %q, statement %q", d, name))
				test.ErrorIs(t, err, ErrUnaddressableRow, test.Sprintf("dialect %q, statement %q", d, name))
				test.StrContains(t, err.Error(), compositeTable, test.Sprintf("dialect %q, statement %q", d, name))
			}
		}
	})

	T.Run("one match is enough, and so is an id on its own", func(t *testing.T) {
		t.Parallel()

		// The requirement is that the statement keys on something, not that it
		// keys on the whole primary key — this package never reads a schema and
		// has no way to know what the whole key is.
		for _, d := range everyDialect() {
			columns := without(compositeColumns(), IDColumn)

			_, keyed := bindQuery(d, For(d).GetQuery("GetSprocket", compositeTable, columns,
				Match{Column: "subject_id"}))
			test.SliceNotEmpty(t, keyed, test.Sprintf("dialect %q", d))

			_, byID := bindQuery(d, For(d).GetQuery("GetGadget", keyedTable, keyedColumns()))
			test.SliceNotEmpty(t, byID, test.Sprintf("dialect %q", d))
		}
	})

	T.Run("the list is unaffected, because a page is not a row", func(t *testing.T) {
		t.Parallel()

		// An unkeyed list addresses a page of a table rather than a row of one,
		// and the filter and the cursor are what bound it.
		for _, d := range everyDialect() {
			err := recovered(func() {
				_ = For(d).ListQuery("ListSprockets", compositeTable, without(compositeColumns(), IDColumn))
			})

			must.NoError(t, err, must.Sprintf("dialect %q", d))
		}
	})
}
