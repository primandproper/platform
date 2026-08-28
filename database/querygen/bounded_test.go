package querygen

import (
	"strings"
	"testing"

	"github.com/primandproper/platform-go/v13/database/dialect"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// The prune and the sweeps both render a bounded write, and what they share is
// asserted here rather than twice over there: which spellings a server accepts,
// how a cap reaches it, and that a bounded statement names an ordering. The
// point of the file is that the two shapes cannot come to disagree about any of
// the three, so most of what follows compares one shape's rendering against the
// other's rather than against a literal.

func TestBoundedWriteForms(T *testing.T) {
	T.Parallel()

	T.Run("names what each server accepts", func(t *testing.T) {
		t.Parallel()

		// MySQL is not the dialect with no bounded write. It is the dialect with
		// a different pair of them, which is the fact the two shapes had written
		// down twice and contradicted each other about.
		for _, d := range []dialect.Dialect{dialect.Postgres, dialect.SQLite} {
			forms := For(d).boundedWriteForms()

			test.True(t, forms.has(selfReferencingSubquery), test.Sprintf("dialect %q", d))
			test.True(t, forms.has(materializedSubquery), test.Sprintf("dialect %q", d))
			test.False(t, forms.has(nativeBound), test.Sprintf("dialect %q", d))
		}

		mysql := For(dialect.MySQL).boundedWriteForms()

		test.False(t, mysql.has(selfReferencingSubquery))
		test.True(t, mysql.has(materializedSubquery))
		test.True(t, mysql.has(nativeBound))
	})

	T.Run("leaves every dialect a spelling", func(t *testing.T) {
		t.Parallel()

		// The materialized subquery is the one all three take, which is what
		// makes it the form a shape falls back to rather than a MySQL special
		// case that happens to work.
		for _, d := range everyDialect() {
			test.True(t, For(d).boundedWriteForms().has(materializedSubquery), test.Sprintf("dialect %q", d))
		}
	})

	T.Run("is what each shape reads its arm off", func(t *testing.T) {
		t.Parallel()

		// The prune takes the native bound where it is offered; the sweep takes
		// the materialized subquery where the self-reference is refused. Same
		// table, opposite answers, and each shape's doc says why.
		prune := For(dialect.MySQL).PruneQuery("PruneSweepings", sweepingsTable, oldestFirst(), pastHorizon()).Content

		test.StrNotContains(t, prune, "SELECT")
		test.StrHasSuffix(t, "LIMIT ?;", prune)

		sweep := For(dialect.MySQL).SweepDeleteQuery("PurgeDueTokens", guardTable,
			guardColumns(), dueOrder(), dueMatches()...).Content

		test.StrContains(t, sweep, "AS "+sweepAlias)

		// And the other two take the self-reference in both shapes: the prune
		// through its aliased capped read, the sweep through the scan directly.
		for _, d := range []dialect.Dialect{dialect.Postgres, dialect.SQLite} {
			test.StrContains(t,
				For(d).PruneQuery("PruneSweepings", sweepingsTable, oldestFirst(), pastHorizon()).Content,
				"FROM "+sweepingsTable+" AS "+doomedAlias, test.Sprintf("dialect %q", d))

			test.StrNotContains(t,
				For(d).SweepDeleteQuery("PurgeDueTokens", guardTable, guardColumns(), dueOrder(), dueMatches()...).Content,
				"AS "+sweepAlias, test.Sprintf("dialect %q", d))
		}
	})
}

func TestGenerator_boundedLimit(T *testing.T) {
	T.Parallel()

	T.Run("spells the cap the way the dialect can carry one", func(t *testing.T) {
		t.Parallel()

		// MySQL takes a placeholder after LIMIT and nothing else, so the
		// argument is dropped there — which is exactly the fact that must not be
		// written down twice, since a copy of it would be the one place a bound
		// stopped reaching a server.
		test.EqOp(t, "LIMIT ?", For(dialect.MySQL).boundedLimit("sqlc.arg(anything)"))

		for _, d := range []dialect.Dialect{dialect.Postgres, dialect.SQLite} {
			test.EqOp(t, "LIMIT sqlc.arg(anything)", For(d).boundedLimit("sqlc.arg(anything)"),
				test.Sprintf("dialect %q", d))
		}
	})

	T.Run("defaults a page size and refuses to default a cap", func(t *testing.T) {
		t.Parallel()

		// The difference between the two bounds is the argument and nothing
		// else: a page left unset is a page of the conventional size, and a cap
		// left unset is the unbounded statement the prune exists to replace.
		for _, d := range []dialect.Dialect{dialect.Postgres, dialect.SQLite} {
			test.StrContains(t, For(d).limitClause(), "COALESCE(sqlc.narg("+LimitArg+")",
				test.Sprintf("dialect %q", d))
			test.EqOp(t, "LIMIT sqlc.arg("+LimitArg+")", For(d).capClause(), test.Sprintf("dialect %q", d))
		}

		// On MySQL the two are the same text, because the grammar has nowhere to
		// put either the name or the default.
		test.EqOp(t, For(dialect.MySQL).limitClause(), For(dialect.MySQL).capClause())
	})

	T.Run("binds both bounds under the one argument name", func(t *testing.T) {
		t.Parallel()

		// "How many rows may this statement touch" is one question however the
		// statement got there, so the prune's cap and the sweep's page size
		// arrive under [LimitArg] on every dialect.
		for _, d := range everyDialect() {
			_, prune := bindArguments(d, For(d).PruneQuery("PruneSweepings", sweepingsTable,
				oldestFirst(), pastHorizon()).Content)
			_, sweep := bindArguments(d, For(d).SweepDeleteQuery("PurgeDueTokens", guardTable,
				guardColumns(), dueOrder(), dueMatches()...).Content)

			test.SliceContains(t, prune, LimitArg, test.Sprintf("dialect %q", d))
			test.SliceContains(t, sweep, LimitArg, test.Sprintf("dialect %q", d))
		}
	})
}

func TestBoundedOrder(T *testing.T) {
	T.Parallel()

	T.Run("hands back the terms it was given", func(t *testing.T) {
		t.Parallel()

		test.Eq(t, dueOrder(), boundedOrder(guardTable, dueOrder()))
	})

	T.Run("refuses a bounded statement that names none", func(t *testing.T) {
		t.Parallel()

		// One error, raised in one place, for both shapes — which is the whole
		// point of it living here rather than beside either of them.
		err := recovered(func() { boundedOrder(guardTable, nil) })

		must.ErrorIs(t, err, ErrUnorderedBoundedStatement)
		test.StrContains(t, err.Error(), guardTable)
	})

	T.Run("is the one answer both shapes give", func(t *testing.T) {
		t.Parallel()

		// The prune used to document an unordered pass as legitimate while the
		// sweep refused one as a programming error. A reader arriving at either
		// doc now learns a rule the other keeps.
		for _, d := range everyDialect() {
			pruned := recovered(func() {
				For(d).PruneQuery("PruneSweepings", sweepingsTable, Prune{Key: []string{IDColumn}}, pastHorizon())
			})
			swept := recovered(func() {
				For(d).SweepDeleteQuery("PurgeDueTokens", guardTable, guardColumns(), nil, dueMatches()...)
			})

			must.ErrorIs(t, pruned, ErrUnorderedBoundedStatement, must.Sprintf("dialect %q", d))
			must.ErrorIs(t, swept, ErrUnorderedBoundedStatement, must.Sprintf("dialect %q", d))
		}
	})

	T.Run("orders every rendering of every bounded statement", func(t *testing.T) {
		t.Parallel()

		// The refusal is only worth having if the ordering it insists on reaches
		// the text. The sweeps render three statements from one scan, so the
		// clause has to appear in each of them.
		for _, d := range everyDialect() {
			for name, content := range map[string]string{
				"prune": For(d).PruneQuery("PruneSweepings", sweepingsTable, oldestFirst(), pastHorizon()).Content,
				"read": For(d).SweepQuery("ListDueTokens", guardTable, guardColumns(),
					Sweep{Order: dueOrder()}, dueMatches()...).Content,
				"delete": For(d).SweepDeleteQuery("PurgeDueTokens", guardTable,
					guardColumns(), dueOrder(), dueMatches()...).Content,
				"update": For(d).SweepUpdateQuery("RedeemDueTokens", guardTable, guardColumns(),
					[]string{"redeemed_at"}, tokenNullable(), dueOrder(), dueMatches()...).Content,
			} {
				test.StrContains(t, content, "ORDER BY ", test.Sprintf("dialect %q, %s", d, name))
			}
		}
	})
}

// TestBoundedWriteDocumentedFacts guards the claim the two shapes had drifted
// apart on: that MySQL has no doomed-subquery form. It has one, this package
// renders it, and a rendering that stopped doing so would leave the corrected
// docs describing a spelling nothing emits.
func TestBoundedWriteDocumentedFacts(T *testing.T) {
	T.Parallel()

	T.Run("renders the materialized subquery MySQL accepts", func(t *testing.T) {
		t.Parallel()

		content := For(dialect.MySQL).SweepDeleteQuery("PurgeDueTokens", guardTable,
			guardColumns(), dueOrder(), dueMatches()...).Content

		// The derived table wraps a scan of the table being written, which is
		// the form ER_UPDATE_TABLE_USED refuses unmaterialized.
		test.StrContains(t, content, "DELETE FROM "+guardTable)
		test.EqOp(t, 2, strings.Count(content, "FROM "+guardTable))
		test.StrContains(t, content, ") AS "+sweepAlias)
	})

	T.Run("keeps the prune's native arm free of a subquery", func(t *testing.T) {
		t.Parallel()

		// The prune declines the materialized form on MySQL, which is a choice
		// rather than the only thing that parses — so the assertion is that the
		// statement is one DELETE and nothing else.
		content := For(dialect.MySQL).PruneQuery("PruneSweepings", sweepingsTable,
			oldestFirst(), pastHorizon()).Content

		test.EqOp(t, 1, strings.Count(content, sweepingsTable))
		test.StrNotContains(t, content, "SELECT")
	})
}
