package querygen

import (
	"slices"
	"strings"
	"testing"

	"github.com/primandproper/platform-go/v13/database/dialect"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// The claim this file exists to check is that a keyed variant is the standard
// statement with more predicates, rather than a second rendering of it.
//
// Most of that claim is structural: every Query form calls the statement
// function StandardCRUD calls, so "the keyed get is the emitted get" is true by
// construction and a test asserting it would only restate the call graph. What
// is left to assert is what a predicate list can get wrong — a key that goes
// missing, an archived clause that follows a projection instead of the column
// list, a guard that binds under the name it is guarding — and that the
// arguments a statement names are the ones a caller can supply.

// keyedTable is the table the statements below are rendered against. It is not
// the sqlc container suite's widgets: a distinct name keeps the two fixtures
// from looking like one.
const keyedTable = "gadgets"

// keyedColumns is a conventional table's column set — every column this package
// has an opinion about, so no predicate is skipped for want of one.
func keyedColumns() []string {
	return []string{
		IDColumn,
		"name",
		BelongsToAccountColumn,
		LastIndexedAtColumn,
		CreatedAtColumn,
		LastUpdatedAtColumn,
		ArchivedAtColumn,
	}
}

// keyedMatch is the extra predicate column most of the pairings below carry, so
// no variant is checked in the one shape that has no matches at all.
func keyedMatch() Match {
	return Match{Column: BelongsToAccountColumn}
}

// dialectForContent is the dialect the assertions about statement *text* are
// made against. Every one of them is over a fragment the three dialects spell
// identically, so asserting it once says what asserting it three times would.
func dialectForContent() dialect.Dialect { return dialect.Postgres }

// keyedQueries is every statement a conventional table keyed on its owner has,
// keyed by name — the corpus such a store would render.
func keyedQueries(d dialect.Dialect) map[string]*Query {
	var (
		g       = For(d)
		columns = keyedColumns()
		owner   = keyedMatch()
	)

	return map[string]*Query{
		"create": g.InsertQuery("CreateGadget", keyedTable, ForInsert(columns), []string{"name"}),
		"get":    g.GetQuery("GetGadget", keyedTable, columns, owner),
		"exists": g.ExistsQuery("CheckGadgetExistence", keyedTable, columns, owner),
		// The owner is out of the updatable set: a statement that assigns the
		// column it keys on binds one argument to both, and there is no value
		// of it that moves a row anywhere.
		"update":  g.UpdateQuery("UpdateGadget", keyedTable, columns, ForUpdate(columns, BelongsToAccountColumn), nil, owner),
		"archive": g.ArchiveQuery("ArchiveGadget", keyedTable, columns, owner),
		"list":    g.ListQuery("ListGadgets", keyedTable, columns, owner),
	}
}

func TestKeyedQueries(T *testing.T) {
	T.Parallel()

	T.Run("agree with themselves about their arguments", func(t *testing.T) {
		t.Parallel()

		// The invariant a driver enforces once the generated code hands one of
		// these over: a marker per value and a value per marker.
		for _, d := range everyDialect() {
			for name, q := range keyedQueries(d) {
				sql, args := bindQuery(d, q)

				assertMarkersMatchArgs(t, d, sql, args)
				test.SliceNotEmpty(t, args, test.Sprintf("dialect %q, statement %q", d, name))
			}
		}
	})

	T.Run("key every single-row statement on the scope as well as the id", func(t *testing.T) {
		t.Parallel()

		// The read path that omits the scope is the one a caller reaches for
		// without having thought about tenancy, so there is deliberately no
		// statement here that has an id predicate and no scope predicate.
		for _, d := range everyDialect() {
			queries := keyedQueries(d)

			for _, name := range []string{"get", "exists", "update", "archive"} {
				_, args := bindQuery(d, queries[name])

				test.SliceContains(t, args, BelongsToAccountColumn,
					test.Sprintf("dialect %q, statement %q", d, name))
				test.SliceContains(t, args, IDColumn,
					test.Sprintf("dialect %q, statement %q", d, name))
			}

			// And the one that addresses a set of rows has no id predicate at
			// all, which is what makes it a page rather than a read.
			_, list := bindQuery(d, queries["list"])
			test.SliceNotContains(t, list, IDColumn, test.Sprintf("dialect %q", d))
		}
	})

	T.Run("exists asks what get asks", func(t *testing.T) {
		t.Parallel()

		// Not the same statement — one reads the row and one reports it — but
		// the same predicates, so a caller cannot be told a row exists and then
		// be refused it.
		for _, d := range everyDialect() {
			queries := keyedQueries(d)

			_, get := bindQuery(d, queries["get"])
			_, exists := bindQuery(d, queries["exists"])

			test.Eq(t, get, exists, test.Sprintf("dialect %q", d))
		}
	})
}

func TestGenerator_GetQuery(T *testing.T) {
	T.Parallel()

	T.Run("is annotated as the single row it reads", func(t *testing.T) {
		t.Parallel()

		q := For(dialectForContent()).GetQuery("GetGadgetForAccount", keyedTable, keyedColumns(), keyedMatch())

		test.EqOp(t, "GetGadgetForAccount", q.Annotation.Name)
		test.EqOp(t, OneType, q.Annotation.Type)
		test.StrContains(t, q.Content, "sqlc.arg("+BelongsToAccountColumn+")")
	})

	T.Run("keys on the extra match columns as well as the id", func(t *testing.T) {
		t.Parallel()

		for _, d := range everyDialect() {
			q := For(d).GetQuery("GetGadgetForAccount", keyedTable, keyedColumns(), keyedMatch())

			sql, args := bindQuery(d, q)

			test.Eq(t, []string{IDColumn, BelongsToAccountColumn}, args, test.Sprintf("dialect %q", d))
			test.StrContains(t, sql, Qualify(keyedTable, BelongsToAccountColumn)+" =", test.Sprintf("dialect %q", d))
			assertMarkersMatchArgs(t, d, sql, args)
		}
	})

	T.Run("excludes archived rows outright", func(t *testing.T) {
		t.Parallel()

		// The single-row reads do not carry the include_archived toggle: a
		// caller wanting an archived row wants a different statement. Losing
		// this predicate is invisible until something reads a row it archived.
		for _, d := range everyDialect() {
			q := For(d).GetQuery("GetGadget", keyedTable, keyedColumns())

			test.StrContains(t, q.Content, Qualify(keyedTable, ArchivedAtColumn)+" IS NULL",
				test.Sprintf("dialect %q", d))
		}
	})
}

func TestGenerator_InsertQuery_NaturalKey(T *testing.T) {
	T.Parallel()

	T.Run("is annotated as the write whose failure raises", func(t *testing.T) {
		t.Parallel()

		q := For(dialectForContent()).InsertQuery("CreateGadget", keyedTable, ForInsert(keyedColumns()), nil)

		test.EqOp(t, "CreateGadget", q.Annotation.Name)
		test.EqOp(t, ExecType, q.Annotation.Type)
		test.StrContains(t, q.Content, "INSERT INTO "+keyedTable)
	})

	T.Run("supplies no value for the database-owned columns", func(t *testing.T) {
		t.Parallel()

		for _, d := range everyDialect() {
			_, args := bindQuery(d, For(d).InsertQuery("CreateGadget", keyedTable, ForInsert(keyedColumns()), nil))

			for _, column := range []string{CreatedAtColumn, LastUpdatedAtColumn, ArchivedAtColumn, LastIndexedAtColumn} {
				test.SliceNotContains(t, args, column, test.Sprintf("dialect %q, column %q", d, column))
			}
		}
	})

	T.Run("serves the table StandardCRUD refuses", func(t *testing.T) {
		t.Parallel()

		// An INSERT keys on nothing, so it is the one statement a natural-key
		// table wants unchanged — and the reason it cannot take StandardCRUD's
		// is the list beside it, which pages by an id the table has not got.
		columns := compositeColumns()

		q := For(dialectForContent()).InsertQuery("CreateSprocket", compositeTable, ForInsert(columns), nil)

		test.StrContains(t, q.Content, "sqlc.arg(subject_type)")
		test.StrContains(t, q.Content, "sqlc.arg(subject_id)")
		test.StrNotContains(t, q.Content, "sqlc.arg("+IDColumn+")")
	})
}

func TestGenerator_ExistsQuery(T *testing.T) {
	T.Parallel()

	T.Run("reports rather than reads", func(t *testing.T) {
		t.Parallel()

		q := For(dialectForContent()).ExistsQuery("CheckGadgetExistenceForAccount", keyedTable, keyedColumns(), keyedMatch())

		test.EqOp(t, "CheckGadgetExistenceForAccount", q.Annotation.Name)
		test.EqOp(t, OneType, q.Annotation.Type)
		test.StrContains(t, q.Content, "SELECT EXISTS (")
	})
}

func TestGenerator_UpdateQuery(T *testing.T) {
	T.Parallel()

	T.Run("counts the rows it matched, because the count is the answer", func(t *testing.T) {
		t.Parallel()

		g := For(dialectForContent())
		q := g.UpdateQuery("UpdateGadgetForAccount", keyedTable, keyedColumns(),
			ForUpdate(keyedColumns(), BelongsToAccountColumn), nil, keyedMatch())

		test.EqOp(t, "UpdateGadgetForAccount", q.Annotation.Name)
		test.EqOp(t, ExecRowsType, q.Annotation.Type)
	})

	T.Run("assigns before it keys, so the argument order is the assignments then the predicates", func(t *testing.T) {
		t.Parallel()

		updates := ForUpdate(keyedColumns(), BelongsToAccountColumn)

		for _, d := range everyDialect() {
			sql, args := bindQuery(d, For(d).UpdateQuery("UpdateGadget", keyedTable, keyedColumns(),
				updates, nil, keyedMatch()))

			test.Eq(t, append(slices.Clone(updates), IDColumn, BelongsToAccountColumn), args,
				test.Sprintf("dialect %q", d))
			assertMarkersMatchArgs(t, d, sql, args)
		}
	})

	T.Run("a column that is both assigned and matched is one argument", func(t *testing.T) {
		t.Parallel()

		// Which is a statement that sets a column to the value it is being
		// required to already hold — legal, useless, and the standard set's
		// behavior too, since WithOwnership renders the owner into the SET and
		// the WHERE from the same argument name. It is named here so that the
		// argument list stops being a surprise: a caller wanting to move a row
		// between owners guards the move with Match.Arg, and one wanting not to
		// assign the column at all keeps it out of the updatable set, which is
		// what ForUpdate's exceptions are for.
		_, args := bindQuery(dialect.Postgres, For(dialect.Postgres).UpdateQuery("UpdateGadget", keyedTable,
			keyedColumns(), ForUpdate(keyedColumns()), nil, keyedMatch()))

		test.SliceContains(t, args, BelongsToAccountColumn)
		test.EqOp(t, 1, strings.Count(strings.Join(args, " "), BelongsToAccountColumn))
	})

	T.Run("a guard on an assigned column binds under its own argument name", func(t *testing.T) {
		t.Parallel()

		// The transfer: the new owner in the SET, the current one in the WHERE,
		// and two arguments rather than one — which is the difference between a
		// statement two concurrent transfers race for and one that assigns the
		// owner it just required the row to already have.
		const current = "current_" + BelongsToAccountColumn

		for _, d := range everyDialect() {
			sql, args := bindQuery(d, For(d).UpdateQuery("TransferGadget", keyedTable, keyedColumns(),
				[]string{BelongsToAccountColumn}, nil, Match{Column: BelongsToAccountColumn, Arg: current}))

			test.Eq(t, []string{BelongsToAccountColumn, IDColumn, current}, args,
				test.Sprintf("dialect %q", d))
			test.StrContains(t, sql, BelongsToAccountColumn+" = ", test.Sprintf("dialect %q", d))
			assertMarkersMatchArgs(t, d, sql, args)
		}
	})
}

func TestGenerator_ArchiveQuery(T *testing.T) {
	T.Parallel()

	T.Run("stamps rather than deletes, and only an unarchived row", func(t *testing.T) {
		t.Parallel()

		for _, d := range everyDialect() {
			g := For(d)
			q := g.ArchiveQuery("ArchiveGadget", keyedTable, keyedColumns())

			test.EqOp(t, ExecRowsType, q.Annotation.Type, test.Sprintf("dialect %q", d))
			test.StrContains(t, q.Content, "UPDATE "+keyedTable, test.Sprintf("dialect %q", d))
			test.StrContains(t, q.Content, ArchivedAtColumn+" = "+g.storedNow(), test.Sprintf("dialect %q", d))
			test.StrContains(t, q.Content, ArchivedAtColumn+" IS NULL", test.Sprintf("dialect %q", d))
			test.StrNotContains(t, q.Content, "DELETE", test.Sprintf("dialect %q", d))
		}
	})
}

func TestGenerator_ReadQuery(T *testing.T) {
	T.Parallel()

	// The table's shape without its id, which is how a read keyed on something
	// other than the row's own id says so — while the projection keeps the
	// column, because the caller still wants it back.
	withoutID := func() []string { return without(keyedColumns(), IDColumn) }

	T.Run("projects and orders what the standard get cannot", func(t *testing.T) {
		t.Parallel()

		read := Read{Projection: []string{IDColumn}, Order: "name"}

		for _, d := range everyDialect() {
			q := For(d).ReadQuery("GetGadgetIDForAccount", keyedTable, withoutID(), read, keyedMatch())

			test.EqOp(t, "GetGadgetIDForAccount", q.Annotation.Name, test.Sprintf("dialect %q", d))
			test.EqOp(t, OneType, q.Annotation.Type, test.Sprintf("dialect %q", d))

			sql, args := bindQuery(d, q)
			assertMarkersMatchArgs(t, d, sql, args)
		}
	})

	T.Run("the projection is what comes back, and the columns are what is keyed on", func(t *testing.T) {
		t.Parallel()

		q := For(dialectForContent()).ReadQuery("GetGadgetName", keyedTable,
			withoutID(), Read{Projection: []string{"name"}}, keyedMatch())

		test.StrContains(t, q.Content, "SELECT\n\t"+Qualify(keyedTable, "name")+"\nFROM")

		// Narrowing the projection does not narrow the predicates: the
		// archived clause comes from the column list, which still has it.
		test.StrContains(t, q.Content, Qualify(keyedTable, ArchivedAtColumn)+" IS NULL")

		// And a column list without an id keys on the matches alone, which is
		// the whole reason a table carrying an id it does not key on can be
		// read at all.
		test.StrNotContains(t, q.Content, "sqlc.arg("+IDColumn+")")
	})

	T.Run("the zero Read projects the column list, which is the standard get", func(t *testing.T) {
		t.Parallel()

		for _, d := range everyDialect() {
			g := For(d)

			test.EqOp(t,
				g.GetQuery("GetGadget", keyedTable, keyedColumns(), keyedMatch()).Content,
				g.ReadQuery("GetGadget", keyedTable, keyedColumns(), Read{}, keyedMatch()).Content,
				test.Sprintf("dialect %q", d))
		}
	})

	T.Run("an order names the row a key admitting several answers with", func(t *testing.T) {
		t.Parallel()

		for _, d := range everyDialect() {
			q := For(d).ReadQuery("GetAnotherGadgetAccount", keyedTable, withoutID(),
				Read{Projection: []string{BelongsToAccountColumn}, Order: BelongsToAccountColumn},
				Match{Column: BelongsToAccountColumn, Exclude: true})

			test.StrContains(t, q.Content,
				"ORDER BY "+Qualify(keyedTable, BelongsToAccountColumn)+" ASC\nLIMIT 1",
				test.Sprintf("dialect %q", d))
		}
	})
}

func TestMatch_Arg(T *testing.T) {
	T.Parallel()

	T.Run("defaults to the column, which is what every keyed read wants", func(t *testing.T) {
		t.Parallel()

		for _, d := range everyDialect() {
			_, args := bindQuery(d, For(d).GetQuery("GetGadget", keyedTable, keyedColumns(), keyedMatch()))

			test.SliceContains(t, args, BelongsToAccountColumn, test.Sprintf("dialect %q", d))
		}
	})

	T.Run("renames the argument without renaming the column", func(t *testing.T) {
		t.Parallel()

		// The predicate still names the column — a guard compares the row's own
		// column against something, and only the something is being named
		// differently.
		sql, args := bindQuery(dialect.Postgres, For(dialect.Postgres).GetQuery("GetGadget", keyedTable,
			keyedColumns(), Match{Column: BelongsToAccountColumn, Arg: "previous_account"}))

		test.StrContains(t, sql, Qualify(keyedTable, BelongsToAccountColumn)+" = ")
		test.Eq(t, []string{IDColumn, "previous_account"}, args)
	})
}

func TestMatch_Exclude(T *testing.T) {
	T.Parallel()

	T.Run("renders the unequal operator against the same bound name", func(t *testing.T) {
		t.Parallel()

		q := For(dialectForContent()).GetQuery("GetOtherGadget", keyedTable, keyedColumns(),
			Match{Column: BelongsToAccountColumn, Exclude: true})

		test.StrContains(t, q.Content,
			Qualify(keyedTable, BelongsToAccountColumn)+" <> sqlc.arg("+BelongsToAccountColumn+")")
	})

	T.Run("an unqualified statement excludes the same way", func(t *testing.T) {
		t.Parallel()

		// The UPDATE statements carry no table qualifier, and the operator is
		// the only thing that should differ between the two renderings.
		g := For(dialectForContent())
		updates := ForUpdate(keyedColumns(), BelongsToAccountColumn)

		q := g.UpdateQuery("UpdateOtherGadgets", keyedTable, keyedColumns(), updates, nil,
			Match{Column: BelongsToAccountColumn, Exclude: true})

		test.StrContains(t, q.Content, BelongsToAccountColumn+" <> sqlc.arg("+BelongsToAccountColumn+")")
	})

	T.Run("the excluded value binds like any other argument", func(t *testing.T) {
		t.Parallel()

		for _, d := range everyDialect() {
			sql, args := bindQuery(d, For(d).GetQuery("GetOtherGadget", keyedTable, keyedColumns(),
				Match{Column: BelongsToAccountColumn, Exclude: true}))

			test.SliceContains(t, args, BelongsToAccountColumn, test.Sprintf("dialect %q", d))
			assertMarkersMatchArgs(t, d, sql, args)
		}
	})
}

func TestKeyedQueriesAreRenderedPerStatement(T *testing.T) {
	T.Parallel()

	T.Run("rendering one does not change what the Generator emits for the next", func(t *testing.T) {
		t.Parallel()

		// Structural — a Query form takes a statement and returns a new one,
		// and the Generator holds nothing a rendering could leave behind.
		// Asserted anyway because the consequence of losing it is a generator
		// binary writing $1 into the .sql files it emits, which is SQL that
		// generates nothing and reads like sqlc broke.
		for _, d := range everyDialect() {
			g := For(d)

			before := getStatement(keyedTable, keyedColumns(), "", Read{})
			_ = g.GetQuery("GetGadget", keyedTable, keyedColumns())
			_ = g.ListQuery("ListGadgets", keyedTable, keyedColumns(), keyedMatch())
			after := getStatement(keyedTable, keyedColumns(), "", Read{})

			test.EqOp(t, before, after, test.Sprintf("dialect %q", d))
			test.StrContains(t, after, "sqlc.arg("+IDColumn+")", test.Sprintf("dialect %q", d))
		}
	})

	T.Run("two renderings of one statement are the same text", func(t *testing.T) {
		t.Parallel()

		g := For(dialect.Postgres)

		first := g.GetQuery("GetGadget", keyedTable, keyedColumns())
		second := g.GetQuery("GetGadget", keyedTable, keyedColumns())

		must.EqOp(t, first.Content, second.Content)
	})
}
