package querygen

import (
	"strings"
	"testing"

	"github.com/primandproper/platform-go/v14/database/dialect"
	platformerrors "github.com/primandproper/platform-go/v14/errors"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// The sweeps render three statements from one scan, so most of what is asserted
// here is that they render the *same* scan: the predicates, the ordering and the
// limit a caller wrote once appear identically in the read and in both writes.
// What the servers make of them is guard_containers_test.go's neighbor — see
// runSweepSuite.

// dueOrder is the ordering every sweep below walks: the deadline, then the id to
// settle the rows that came due in the same instant.
func dueOrder() []Order {
	return []Order{{Column: "expires_at"}, {Column: IDColumn}}
}

// dueMatches is the predicate the sweeps share: a row past a bound instant that
// has not already been dealt with.
func dueMatches() []Match {
	return []Match{
		{Column: "redeemed_at", Against: NoValue},
		{Column: "expires_at", Against: AtMostArgument, Arg: "expires_before"},
	}
}

func TestGenerator_SweepQuery(T *testing.T) {
	T.Parallel()

	T.Run("is a bounded ordered read", func(t *testing.T) {
		t.Parallel()

		for _, d := range everyDialect() {
			g := For(d)
			q := g.SweepQuery("ListDueTokens", guardTable, guardColumns(),
				Sweep{Order: dueOrder()}, dueMatches()...)

			test.EqOp(t, "ListDueTokens", q.Annotation.Name)

			// :many, because a sweep hands its rows to something that has work
			// to do with each of them.
			test.EqOp(t, ManyType, q.Annotation.Type)

			test.StrContains(t, q.Content,
				"ORDER BY "+Qualify(guardTable, "expires_at")+" ASC, "+Qualify(guardTable, IDColumn)+" ASC",
				test.Sprintf("dialect %q", d))
			test.StrContains(t, q.Content, g.limitClause(), test.Sprintf("dialect %q", d))
		}
	})

	// A sweep is not a list, and the difference is not a matter of degree. A
	// filter window would let a caller's unrelated date range decide which
	// expired rows get collected, and a cursor would be a position held between
	// passes over rows that are no longer due by the next one.
	T.Run("carries no filter window, no cursor and no counts", func(t *testing.T) {
		t.Parallel()

		for _, d := range everyDialect() {
			q := For(d).SweepQuery("ListDueTokens", guardTable, guardColumns(),
				Sweep{Order: dueOrder()}, dueMatches()...)

			for _, absent := range []string{
				CreatedAfterArg, CreatedBeforeArg, UpdatedAfterArg, UpdatedBeforeArg,
				IncludeArchivedArg, CursorArg, "filtered_count", "total_count",
			} {
				test.StrNotContains(t, q.Content, absent,
					test.Sprintf("dialect %q renders %s", d, absent))
			}
		}
	})

	// The same rule every read in this package follows: what the column list
	// says the table has is what gets a predicate.
	T.Run("the column list decides the archived predicate", func(t *testing.T) {
		t.Parallel()

		for _, d := range everyDialect() {
			g := For(d)

			with := g.SweepQuery("ListDueTokens", guardTable, guardColumns(),
				Sweep{Order: dueOrder()}, dueMatches()...)
			test.StrContains(t, with.Content, Qualify(guardTable, ArchivedAtColumn)+" IS NULL",
				test.Sprintf("dialect %q", d))

			without := g.SweepQuery("ListDueTokens", guardTable,
				withoutColumns(guardColumns(), ArchivedAtColumn),
				Sweep{Order: dueOrder()}, dueMatches()...)
			test.StrNotContains(t, without.Content, ArchivedAtColumn,
				test.Sprintf("dialect %q", d))
		}
	})

	// A sweep addresses a set. A statement keyed on the row's own id would be a
	// sweep of exactly one row, which is the get with extra machinery.
	T.Run("renders no id predicate", func(t *testing.T) {
		t.Parallel()

		for _, d := range everyDialect() {
			q := For(d).SweepQuery("ListDueTokens", guardTable, guardColumns(),
				Sweep{Order: dueOrder()}, dueMatches()...)

			test.StrNotContains(t, q.Content, "sqlc.arg(id)", test.Sprintf("dialect %q", d))
		}
	})

	T.Run("projects what the sweep names", func(t *testing.T) {
		t.Parallel()

		for _, d := range everyDialect() {
			q := For(d).SweepQuery("ListDueTokenIDs", guardTable, guardColumns(),
				Sweep{Order: dueOrder(), Projection: []string{IDColumn}}, dueMatches()...)

			header, _, found := strings.Cut(q.Content, "\nFROM")
			must.True(t, found)
			test.StrNotContains(t, header, "secret", test.Sprintf("dialect %q", d))
			test.StrContains(t, header, Qualify(guardTable, IDColumn), test.Sprintf("dialect %q", d))
		}
	})
}

func TestGenerator_SweepWrites(T *testing.T) {
	T.Parallel()

	T.Run("delete and update address the rows the read would return", func(t *testing.T) {
		t.Parallel()

		for _, d := range everyDialect() {
			g := For(d)

			read := g.SweepQuery("ListDueTokens", guardTable,
				withoutColumns(guardColumns(), ArchivedAtColumn),
				Sweep{Order: dueOrder(), Projection: []string{IDColumn}}, dueMatches()...)

			del := g.SweepDeleteQuery("PurgeDueTokens", guardTable,
				withoutColumns(guardColumns(), ArchivedAtColumn), dueOrder(), dueMatches()...)

			// The read's whole text is the write's subquery, modulo the
			// indentation and — on MySQL — the derived table around it. So the
			// predicates cannot drift between the pass that reports and the
			// pass that acts.
			for line := range strings.SplitSeq(strings.TrimSuffix(read.Content, ";"), "\n") {
				test.StrContains(t, del.Content, strings.TrimSpace(line),
					test.Sprintf("dialect %q", d))
			}
		}
	})

	T.Run("both report the rows they collected", func(t *testing.T) {
		t.Parallel()

		for _, d := range everyDialect() {
			g := For(d)

			del := g.SweepDeleteQuery("PurgeDueTokens", guardTable, guardColumns(), dueOrder(), dueMatches()...)
			test.EqOp(t, ExecRowsType, del.Annotation.Type)

			update := g.SweepUpdateQuery("RedeemDueTokens", guardTable, guardColumns(),
				[]string{"redeemed_at"}, tokenNullable(), dueOrder(), dueMatches()...)
			test.EqOp(t, ExecRowsType, update.Annotation.Type)
		}
	})

	// SQLite resolves a bare `id` here against both the statement's target and
	// the subquery's table and calls it ambiguous, which is a compile error on
	// one dialect for a statement the other two accept.
	T.Run("qualify the outer key", func(t *testing.T) {
		t.Parallel()

		for _, d := range everyDialect() {
			q := For(d).SweepDeleteQuery("PurgeDueTokens", guardTable, guardColumns(), dueOrder(), dueMatches()...)

			test.StrContains(t, q.Content, "WHERE "+Qualify(guardTable, IDColumn)+" IN (",
				test.Sprintf("dialect %q", d))
		}
	})

	// MySQL refuses a subquery reading the table being written
	// (ER_UPDATE_TABLE_USED) and accepts the identical rows once they have been
	// materialized. The other two take the scan directly, and a derived table
	// there would be a planner obstacle for no reason.
	T.Run("materialize the scan for MySQL and nowhere else", func(t *testing.T) {
		t.Parallel()

		for _, d := range everyDialect() {
			for _, q := range []*Query{
				For(d).SweepDeleteQuery("PurgeDueTokens", guardTable, guardColumns(), dueOrder(), dueMatches()...),
				For(d).SweepUpdateQuery("RedeemDueTokens", guardTable, guardColumns(),
					[]string{"redeemed_at"}, tokenNullable(), dueOrder(), dueMatches()...),
			} {
				if d == dialect.MySQL {
					test.StrContains(t, q.Content, ") AS "+sweepAlias, test.Sprintf("%s/%s", d, q.Annotation.Name))
				} else {
					test.StrNotContains(t, q.Content, sweepAlias, test.Sprintf("%s/%s", d, q.Annotation.Name))
				}
			}
		}
	})

	// The stamp is the convention's, not this shape's: a bounded write is an
	// update, and every update in this package stamps the column where the
	// table has one.
	T.Run("the update stamps last_updated_at", func(t *testing.T) {
		t.Parallel()

		for _, d := range everyDialect() {
			g := For(d)
			q := g.SweepUpdateQuery("RedeemDueTokens", guardTable, guardColumns(),
				[]string{"redeemed_at"}, tokenNullable(), dueOrder(), dueMatches()...)

			test.StrContains(t, q.Content, LastUpdatedAtColumn+" = "+g.storedNow(),
				test.Sprintf("dialect %q", d))
		}
	})

	// A bounded write over a table with no id has no set to name its rows
	// through, and there is nothing here to invent one from.
	T.Run("refuse a table with no id", func(t *testing.T) {
		t.Parallel()

		err := recoverPanic(t, func() {
			For(dialect.Postgres).SweepDeleteQuery("PurgeGrants", grantsTable,
				withoutColumns(grantsColumns(), IDColumn),
				[]Order{{Column: ArchivedAtColumn}},
				Match{Column: grantOwnerColumn})
		})

		test.ErrorIs(t, err, ErrMissingIDColumn)
	})
}

func TestGenerator_SweepRefusals(T *testing.T) {
	T.Parallel()

	// A bounded DELETE over no predicate is a truncate paid for in installments,
	// and the LIMIT beside it makes that look deliberate.
	T.Run("a sweep with no predicate", func(t *testing.T) {
		t.Parallel()

		for _, build := range []func(*Generator){
			func(g *Generator) {
				g.SweepQuery("ListEverything", guardTable, guardColumns(), Sweep{Order: dueOrder()})
			},
			func(g *Generator) {
				g.SweepDeleteQuery("PurgeEverything", guardTable, guardColumns(), dueOrder())
			},
			func(g *Generator) {
				g.SweepUpdateQuery("StampEverything", guardTable, guardColumns(),
					[]string{"redeemed_at"}, tokenNullable(), dueOrder())
			},
		} {
			err := recoverPanic(t, func() { build(For(dialect.Postgres)) })
			test.ErrorIs(t, err, ErrUnpredicatedStatement)
		}
	})

	// A LIMIT with no ORDER BY takes whichever rows the server produced first,
	// which can differ between two runs over the same rows — and can pass over
	// the oldest row forever while newer ones behind it are collected. All three
	// sweeps render through one scan, so all three refuse it: the two writes are
	// the ones where the non-determinism leaves nothing behind to inspect.
	T.Run("a sweep with no ordering", func(t *testing.T) {
		t.Parallel()

		for _, build := range []func(*Generator){
			func(g *Generator) {
				g.SweepQuery("ListDueTokens", guardTable, guardColumns(), Sweep{}, dueMatches()...)
			},
			func(g *Generator) {
				g.SweepDeleteQuery("PurgeDueTokens", guardTable, guardColumns(), nil, dueMatches()...)
			},
			func(g *Generator) {
				g.SweepUpdateQuery("RedeemDueTokens", guardTable, guardColumns(),
					[]string{"redeemed_at"}, tokenNullable(), nil, dueMatches()...)
			},
		} {
			err := recoverPanic(t, func() { build(For(dialect.Postgres)) })

			test.ErrorIs(t, err, ErrUnorderedBoundedStatement)
		}
	})
}

func TestGenerator_CountQuery(T *testing.T) {
	T.Parallel()

	T.Run("counts rows under the sweep's predicates", func(t *testing.T) {
		t.Parallel()

		for _, d := range everyDialect() {
			q := For(d).CountQuery("CountDueTokens", guardTable, guardColumns(), dueMatches()...)

			test.EqOp(t, "CountDueTokens", q.Annotation.Name)
			test.EqOp(t, OneType, q.Annotation.Type)

			test.StrHasPrefix(t, "SELECT COUNT(*)", q.Content, test.Sprintf("dialect %q", d))
			test.StrContains(t, q.Content, Qualify(guardTable, ArchivedAtColumn)+" IS NULL",
				test.Sprintf("dialect %q", d))
			test.StrContains(t, q.Content, Qualify(guardTable, "redeemed_at")+" IS NULL",
				test.Sprintf("dialect %q", d))
		}
	})

	// A count keyed on the row's own id answers one or zero, which is the
	// existence check with more steps. Unlike the get's id predicate this one is
	// not the column list's decision: the shape declines it outright, so a
	// caller handing over the table's own columns still gets a count over the
	// set its matches name.
	T.Run("renders no id predicate, whatever the column list says", func(t *testing.T) {
		t.Parallel()

		for _, d := range everyDialect() {
			for _, columns := range [][]string{guardColumns(), withoutColumns(guardColumns(), IDColumn)} {
				q := For(d).CountQuery("CountDueTokens", guardTable, columns, dueMatches()...)
				test.StrNotContains(t, q.Content, "sqlc.arg(id)", test.Sprintf("dialect %q", d))
			}
		}
	})

	// A count over no predicate is a number about every row a database holds for
	// everybody, which is the one number a tenancy-scoped schema has no caller
	// for.
	T.Run("refuses an unpredicated count", func(t *testing.T) {
		t.Parallel()

		err := recoverPanic(t, func() {
			For(dialect.Postgres).CountQuery("CountTokens", guardTable, guardColumns())
		})

		test.ErrorIs(t, err, ErrUnpredicatedStatement)
	})
}

func TestMatch_AtMostArgument(T *testing.T) {
	T.Parallel()

	// The complement of "at or before the bound" is "strictly after it", so the
	// two forms partition the rows rather than overlapping at the instant a
	// deadline falls on — exactly as CurrentTime's two forms do, because they
	// are the same boundary read off two different clocks.
	T.Run("partitions the rows at its boundary", func(t *testing.T) {
		t.Parallel()

		for _, d := range everyDialect() {
			g := For(d)

			swept := g.matchPredicate(guardTable, Match{Column: "expires_at", Against: AtMostArgument}, true)
			test.EqOp(t, Qualify(guardTable, "expires_at")+" <= sqlc.arg(expires_at)", swept)

			live := g.matchPredicate(guardTable,
				Match{Column: "expires_at", Against: AtMostArgument, Exclude: true}, true)
			test.EqOp(t, Qualify(guardTable, "expires_at")+" > sqlc.arg(expires_at)", live)

			_ = d
		}
	})

	T.Run("binds the argument the match names", func(t *testing.T) {
		t.Parallel()

		predicate := For(dialect.Postgres).matchPredicate(guardTable,
			Match{Column: "expires_at", Against: AtMostArgument, Arg: "expires_before"}, false)

		test.EqOp(t, "expires_at <= sqlc.arg(expires_before)", predicate)
	})

	T.Run("names itself for the misuse messages", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, "a bound ceiling", AtMostArgument.String())
	})
}

// recoverPanic runs build and returns the error it panicked with, failing the
// test when it did not panic at all. Every misuse check in this package panics,
// because its arguments are string literals in a generator binary.
func recoverPanic(t *testing.T, build func()) (err error) {
	t.Helper()

	defer func() {
		recovered := recover()
		must.NotNil(t, recovered, must.Sprint("expected a panic"))

		panicked, ok := recovered.(error)
		must.True(t, ok, must.Sprintf("panicked with %v", recovered))

		err = panicked
	}()

	build()

	return platformerrors.New("unreachable")
}

// withoutColumns is the tests' spelling of what a caller does to leave a
// predicate off: hand over a column list without the column it derives from.
func withoutColumns(columns []string, excluded ...string) []string {
	return without(columns, excluded...)
}
