package querygen

import (
	"strings"
	"testing"

	"github.com/primandproper/platform-go/v13/database/dialect"

	"github.com/shoenig/test"
)

// The claim this file exists to check is ListQuery's, for its four siblings:
// each canonical Query form is its Bound counterpart's source text, named and
// annotated. Rewriting a Query's argument references for a driver must yield
// exactly the SQL the Bound method executes — if the two ever render
// differently, a keyed variant in a consumer's canonical .sql is checked by
// sqlc while a different statement runs against the database.
//
// The pairing is structural: each Query form calls the statement function its
// Bound counterpart calls. What the assertions add is the property that would
// break if somebody gave one of them a second rendering — which is the way this
// gap opened the first time.

// keyedMatch is the extra predicate column every pairing below carries, so no
// variant is checked in the one shape that has no matches at all.
func keyedMatch() Match {
	return Match{Column: BelongsToAccountColumn}
}

func TestGenerator_GetQuery(T *testing.T) {
	T.Parallel()

	for _, d := range everyDialect() {
		T.Run(string(d), func(t *testing.T) {
			t.Parallel()

			g := For(d)
			q := g.GetQuery("GetGadgetForAccount", boundTable, boundColumns(), keyedMatch())

			test.EqOp(t, "GetGadgetForAccount", q.Annotation.Name)
			test.EqOp(t, OneType, q.Annotation.Type)
			test.StrContains(t, q.Content, "sqlc.arg("+BelongsToAccountColumn+")")

			assertSameStatement(t, d, q, g.BoundGet(boundTable, boundColumns(), keyedMatch()))
		})
	}
}

func TestGenerator_ExistsQuery(T *testing.T) {
	T.Parallel()

	for _, d := range everyDialect() {
		T.Run(string(d), func(t *testing.T) {
			t.Parallel()

			g := For(d)
			q := g.ExistsQuery("CheckGadgetExistenceForAccount", boundTable, boundColumns(), keyedMatch())

			test.EqOp(t, "CheckGadgetExistenceForAccount", q.Annotation.Name)
			test.EqOp(t, OneType, q.Annotation.Type)
			test.StrContains(t, q.Content, "SELECT EXISTS (")

			assertSameStatement(t, d, q, g.BoundExists(boundTable, boundColumns(), keyedMatch()))
		})
	}
}

func TestGenerator_UpdateQuery(T *testing.T) {
	T.Parallel()

	for _, d := range everyDialect() {
		T.Run(string(d), func(t *testing.T) {
			t.Parallel()

			g := For(d)
			updates := ForUpdate(boundColumns(), BelongsToAccountColumn)

			q := g.UpdateQuery("UpdateGadgetForAccount", boundTable, boundColumns(), updates, nil, keyedMatch())

			test.EqOp(t, "UpdateGadgetForAccount", q.Annotation.Name)

			// :execrows rather than :exec — the count is how a caller learns
			// the row it aimed at was already gone.
			test.EqOp(t, ExecRowsType, q.Annotation.Type)

			assertSameStatement(t, d, q, g.BoundUpdate(boundTable, boundColumns(), updates, nil, keyedMatch()))
		})
	}
}

func TestGenerator_ArchiveQuery(T *testing.T) {
	T.Parallel()

	for _, d := range everyDialect() {
		T.Run(string(d), func(t *testing.T) {
			t.Parallel()

			g := For(d)
			q := g.ArchiveQuery("ArchiveGadgetForAccount", boundTable, boundColumns(), keyedMatch())

			test.EqOp(t, "ArchiveGadgetForAccount", q.Annotation.Name)
			test.EqOp(t, ExecRowsType, q.Annotation.Type)
			test.StrContains(t, q.Content, ArchivedAtColumn+" = "+g.storedNow())

			assertSameStatement(t, d, q, g.BoundArchive(boundTable, boundColumns(), keyedMatch()))
		})
	}
}

func TestGenerator_ReadQuery(T *testing.T) {
	T.Parallel()

	// The table's shape without its id, which is how a read keyed on something
	// other than the row's own id says so — while the projection keeps the
	// column, because the caller still wants it back.
	keyedColumns := func() []string { return without(boundColumns(), IDColumn) }

	T.Run("pairs with BoundRead", func(t *testing.T) {
		t.Parallel()

		read := Read{Projection: []string{IDColumn}, Order: "name"}

		for _, d := range everyDialect() {
			g := For(d)
			q := g.ReadQuery("GetGadgetIDForAccount", boundTable, keyedColumns(), read, keyedMatch())

			test.EqOp(t, "GetGadgetIDForAccount", q.Annotation.Name)
			test.EqOp(t, OneType, q.Annotation.Type)

			assertSameStatement(t, d, q, g.BoundRead(boundTable, keyedColumns(), read, keyedMatch()))
		}
	})

	T.Run("the projection is what comes back, and the columns are what is keyed on", func(t *testing.T) {
		t.Parallel()

		q := For(dialectForContent()).ReadQuery("GetGadgetName", boundTable,
			keyedColumns(), Read{Projection: []string{"name"}}, keyedMatch())

		test.StrContains(t, q.Content, "SELECT\n\t"+Qualify(boundTable, "name")+"\nFROM")

		// Narrowing the projection does not narrow the predicates: the
		// archived clause comes from the column list, which still has it.
		test.StrContains(t, q.Content, Qualify(boundTable, ArchivedAtColumn)+" IS NULL")

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
				g.GetQuery("GetGadget", boundTable, boundColumns(), keyedMatch()).Content,
				g.ReadQuery("GetGadget", boundTable, boundColumns(), Read{}, keyedMatch()).Content,
				test.Sprintf("dialect %q", d))
		}
	})

	T.Run("an order names the row a key admitting several answers with", func(t *testing.T) {
		t.Parallel()

		for _, d := range everyDialect() {
			q := For(d).ReadQuery("GetAnotherGadgetAccount", boundTable, keyedColumns(),
				Read{Projection: []string{BelongsToAccountColumn}, Order: BelongsToAccountColumn},
				Match{Column: BelongsToAccountColumn, Exclude: true})

			test.StrContains(t, q.Content,
				"ORDER BY "+Qualify(boundTable, BelongsToAccountColumn)+" ASC\nLIMIT 1",
				test.Sprintf("dialect %q", d))
		}
	})
}

func TestMatch_Exclude(T *testing.T) {
	T.Parallel()

	T.Run("renders the unequal operator against the same bound name", func(t *testing.T) {
		t.Parallel()

		q := For(dialectForContent()).GetQuery("GetOtherGadget", boundTable, boundColumns(),
			Match{Column: BelongsToAccountColumn, Exclude: true})

		test.StrContains(t, q.Content,
			Qualify(boundTable, BelongsToAccountColumn)+" <> sqlc.arg("+BelongsToAccountColumn+")")
	})

	T.Run("an unqualified statement excludes the same way", func(t *testing.T) {
		t.Parallel()

		// The UPDATE statements carry no table qualifier, and the operator is
		// the only thing that should differ between the two renderings.
		g := For(dialectForContent())
		updates := ForUpdate(boundColumns(), BelongsToAccountColumn)

		q := g.UpdateQuery("UpdateOtherGadgets", boundTable, boundColumns(), updates, nil,
			Match{Column: BelongsToAccountColumn, Exclude: true})

		test.StrContains(t, q.Content, BelongsToAccountColumn+" <> sqlc.arg("+BelongsToAccountColumn+")")
	})

	T.Run("the excluded value binds like any other argument", func(t *testing.T) {
		t.Parallel()

		for _, d := range everyDialect() {
			b := For(d).BoundGet(boundTable, boundColumns(),
				Match{Column: BelongsToAccountColumn, Exclude: true})

			test.SliceContains(t, b.Args, BelongsToAccountColumn, test.Sprintf("dialect %q", d))
			assertMarkersMatchArgs(t, d, b)
		}
	})
}

// dialectForContent is the dialect the assertions about statement *text* are
// made against. Every one of them is over a fragment the three dialects spell
// identically, so asserting it once says what asserting it three times would.
func dialectForContent() dialect.Dialect { return dialect.Postgres }

// assertSameStatement pins a canonical Query and its Bound counterpart to the
// same statement: rewriting the Query's argument references for d yields the
// Bound method's SQL and its argument order, byte for byte.
func assertSameStatement(tb testing.TB, d dialect.Dialect, q *Query, bound Bound) {
	tb.Helper()

	// The canonical spelling carries argument references rather than markers,
	// which is what makes the rewrite below a rewrite rather than a no-op.
	test.False(tb, strings.Contains(q.Content, "$1"), test.Sprintf("dialect %q", d))

	sql, args := bindArguments(d, q.Content)

	test.EqOp(tb, bound.SQL, sql, test.Sprintf("dialect %q", d))
	test.Eq(tb, bound.Args, args, test.Sprintf("dialect %q", d))
}
