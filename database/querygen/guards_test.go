package querygen

import (
	"strings"
	"testing"

	"github.com/primandproper/platform-go/v13/database/dialect"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// A guard is a predicate whose right-hand side belongs to the statement rather
// than to the caller, and the reason this file is separate from bind_test.go is
// that the property being checked is different. There, the claim is that a bound
// statement is the emitted one with its references rewritten. Here it is that a
// comparand renders one boundary, that Exclude complements it rather than asking
// a second question, and that the three comparands binding nothing bind nothing
// — because a guard a caller can supply an argument for is a guard a caller can
// disarm.

// guardTable is what the renderings below key on. Its columns are a token's:
// something to spend, a stamp saying it was spent, and a deadline — which is
// the shape all three guard comparands were added for.
const guardTable = "tokens"

func guardColumns() []string {
	return []string{
		IDColumn,
		"secret",
		"redeemed_at",
		"expires_at",
		CreatedAtColumn,
		LastUpdatedAtColumn,
		ArchivedAtColumn,
	}
}

// guardPredicate renders one match the way a keyed read carries it, which is the
// qualified form every read uses.
func guardPredicate(t *testing.T, d dialect.Dialect, match Match) string {
	t.Helper()

	return For(d).matchPredicate(guardTable, match, true)
}

func TestComparand_Renders(T *testing.T) {
	T.Parallel()

	cases := map[string]struct {
		want  string
		match Match
	}{
		"a bound argument is the zero value": {
			match: Match{Column: "secret"},
			want:  "tokens.secret = sqlc.arg(secret)",
		},
		"a bound argument, excluded": {
			match: Match{Column: "secret", Exclude: true},
			want:  "tokens.secret <> sqlc.arg(secret)",
		},
		"NULL is the stamp that has not been written": {
			match: Match{Column: "redeemed_at", Against: NoValue},
			want:  "tokens.redeemed_at IS NULL",
		},
		"NULL excluded is the stamp that has": {
			match: Match{Column: "redeemed_at", Against: NoValue, Exclude: true},
			want:  "tokens.redeemed_at IS NOT NULL",
		},
		"the empty string is the column holding nothing": {
			match: Match{Column: "secret", Against: EmptyString},
			want:  "tokens.secret = ''",
		},
		"the empty string excluded is the not-empty guard": {
			match: Match{Column: "secret", Against: EmptyString, Exclude: true},
			want:  "tokens.secret <> ''",
		},
		"the clock at or before now is the expiry sweep": {
			match: Match{Column: "expires_at", Against: CurrentTime},
			want:  "tokens.expires_at <= " + NowExpression,
		},
		"the clock excluded is still live": {
			match: Match{Column: "expires_at", Against: CurrentTime, Exclude: true},
			want:  "tokens.expires_at > " + NowExpression,
		},
		"a bound time at or before the horizon is the sweep": {
			match: Match{Column: "expires_at", Against: BoundTime},
			want:  "tokens.expires_at <= sqlc.arg(expires_at)",
		},
		"a bound time excluded is still live at that instant": {
			match: Match{Column: "expires_at", Against: BoundTime, Exclude: true},
			want:  "tokens.expires_at > sqlc.arg(expires_at)",
		},
		"a bound time takes the name the match gives it": {
			match: Match{Column: "expires_at", Against: BoundTime, Arg: "now"},
			want:  "tokens.expires_at <= sqlc.arg(now)",
		},
		"an optional argument coalesces to the empty string": {
			match: Match{Column: IDColumn, Against: OptionalArgument, Exclude: true},
			want:  "tokens.id <> COALESCE(sqlc.narg(id), '')",
		},
		"an optional argument takes the name the match gives it": {
			match: Match{Column: IDColumn, Against: OptionalArgument, Arg: "except_id", Exclude: true},
			want:  "tokens.id <> COALESCE(sqlc.narg(except_id), '')",
		},
	}

	for name, testCase := range cases {
		T.Run(name, func(t *testing.T) {
			t.Parallel()

			// Postgres, because the spellings above are the ones all three
			// share — the one that does not is the clock, which has its own
			// test below.
			test.EqOp(t, testCase.want, guardPredicate(t, dialect.Postgres, testCase.match))
		})
	}
}

func TestComparand_GuardsBindNothing(T *testing.T) {
	T.Parallel()

	T.Run("no argument reference reaches a statement a guard is the only match of", func(t *testing.T) {
		t.Parallel()

		// The point of the closed set is that these three compare against
		// something the statement owns. An argument reference here would be one
		// a caller could leave unset, and an unset argument is a guard that
		// admits every row rather than none.
		for _, comparand := range []Comparand{NoValue, EmptyString, CurrentTime} {
			for _, d := range everyDialect() {
				got := For(d).matchPredicate(guardTable, Match{Column: "redeemed_at", Against: comparand}, true)

				test.StrNotContains(t, got, "sqlc.", test.Sprintf("dialect %q comparand %q", d, comparand))
				test.StrNotContains(t, got, "?", test.Sprintf("dialect %q comparand %q", d, comparand))
			}
		}
	})

	T.Run("the optional argument is the one guard form that does bind", func(t *testing.T) {
		t.Parallel()

		// It is nullable rather than required, which is the whole point: the
		// caller who has no row to exclude sends nothing and the statement is
		// still the same statement.
		got := For(dialect.Postgres).ReadQuery("GetTokenID", guardTable, nil,
			Read{Projection: []string{IDColumn}},
			Match{Column: "secret"},
			Match{Column: IDColumn, Against: OptionalArgument, Arg: "except_id", Exclude: true})

		test.StrContains(t, got.Content, "sqlc.narg(except_id)")
		test.StrNotContains(t, got.Content, "sqlc.arg(except_id)")
	})
}

func TestComparand_ClockComesFromOneScreen(T *testing.T) {
	T.Parallel()

	T.Run("a comparison asks for the time in the units the assignments write it in", func(t *testing.T) {
		t.Parallel()

		// MySQL's bare CURRENT_TIMESTAMP is second-granular whatever the column
		// holds, so a comparison spelled without the fractional form decides a
		// DATETIME(6) deadline by a truncated now — a row stays live for up to a
		// second past its expiry. storedNow is where that divergence lives, and
		// routing the comparison through it is what keeps it on one screen.
		for _, d := range everyDialect() {
			g := For(d)

			got := g.matchPredicate(guardTable, Match{Column: "expires_at", Against: CurrentTime}, true)

			test.StrContains(t, got, g.storedNow(), test.Sprintf("dialect %q", d))
		}

		test.StrContains(t,
			For(dialect.MySQL).matchPredicate(guardTable, Match{Column: "expires_at", Against: CurrentTime}, true),
			NowExpression+"(6)")
	})
}

// TestComparand_BoundTimeIsTheClockMoved is the property that makes the two
// temporal comparands one decision rather than two spellings: they render the same
// operator on the same side of the boundary, and differ only in what stands to
// the right of it.
//
// It matters because the pair is what a sweep and the guard refusing to spend a
// row are written from. A bound horizon that read the boundary the other way
// round would leave a row expired to the sweep and live to the guard at exactly
// the instant its deadline falls on.
func TestComparand_BoundTimeIsTheClockMoved(T *testing.T) {
	T.Parallel()

	T.Run("the same boundary, against the caller's reading of the time", func(t *testing.T) {
		t.Parallel()

		for _, d := range everyDialect() {
			g := For(d)

			for _, exclude := range []bool{false, true} {
				clock := g.matchPredicate(guardTable,
					Match{Column: "expires_at", Against: CurrentTime, Exclude: exclude}, true)
				bound := g.matchPredicate(guardTable,
					Match{Column: "expires_at", Against: BoundTime, Arg: "now", Exclude: exclude}, true)

				test.EqOp(t, strings.Replace(clock, g.storedNow(), "sqlc.arg(now)", 1), bound,
					test.Sprintf("dialect %q excluded %v", d, exclude))
			}
		}
	})

	// Required rather than nullable, unlike the other presence-conditional
	// binding form. An unset horizon is not a wider sweep; it is a comparison
	// against NULL, which matches no row and reports the sweep as having found
	// nothing to do.
	T.Run("binds a required argument", func(t *testing.T) {
		t.Parallel()

		got := For(dialect.Postgres).DeleteQuery("SweepTokens", guardTable, nil,
			Match{Column: "expires_at", Against: BoundTime, Arg: "now"})

		test.StrContains(t, got.Content, "sqlc.arg(now)")
		test.StrNotContains(t, got.Content, "sqlc.narg")
		test.StrNotContains(t, got.Content, NowExpression)
	})
}

func TestComparand_ExcludeComplements(T *testing.T) {
	T.Parallel()

	T.Run("the two directions of a comparand partition the rows", func(t *testing.T) {
		t.Parallel()

		// A guard and the sweep that collects what it refuses are one Match with
		// one bool between them. If the two spellings were written separately
		// they could come to disagree about the boundary, and the rows in the
		// gap would be neither live nor expired.
		for _, comparand := range []Comparand{
			BoundArgument, NoValue, EmptyString, CurrentTime, BoundTime, OptionalArgument,
		} {
			included := guardPredicate(t, dialect.Postgres, Match{Column: "expires_at", Against: comparand})
			excluded := guardPredicate(t, dialect.Postgres,
				Match{Column: "expires_at", Against: comparand, Exclude: true})

			test.NotEqOp(t, included, excluded, test.Sprintf("comparand %q", comparand))

			// Both name the same column: Exclude inverts the comparison, never
			// the thing being compared.
			test.StrContains(t, included, Qualify(guardTable, "expires_at"), test.Sprintf("comparand %q", comparand))
			test.StrContains(t, excluded, Qualify(guardTable, "expires_at"), test.Sprintf("comparand %q", comparand))
		}
	})
}

func TestComparand_ComposesWithEveryStatement(T *testing.T) {
	T.Parallel()

	// A guard is not a shape of its own; it is a Match, so it reaches every
	// statement a Match reaches. Each of these renders through a different
	// builder — a read, an existence check, an update, an archive, a list — and
	// a comparand that only worked in one of them would be a comparand a store
	// could not put a write's correctness on.
	unredeemed := Match{Column: "redeemed_at", Against: NoValue}
	live := Match{Column: "expires_at", Against: CurrentTime, Exclude: true}

	rendered := map[string]string{}

	for _, d := range everyDialect() {
		g := For(d)

		rendered[string(d)+"/read"] = g.GetQuery("GetToken", guardTable, guardColumns(), unredeemed, live).Content
		rendered[string(d)+"/exists"] = g.ExistsQuery("TokenExists", guardTable, guardColumns(), unredeemed, live).Content
		rendered[string(d)+"/update"] = g.UpdateQuery("RedeemToken", guardTable, guardColumns(),
			[]string{"redeemed_at"}, []string{"redeemed_at"}, unredeemed, live).Content
		rendered[string(d)+"/archive"] = g.ArchiveQuery("ArchiveToken", guardTable, guardColumns(), unredeemed, live).Content
		rendered[string(d)+"/list"] = pagedList(g.ListQueries("ListTokens", guardTable, guardColumns(), unredeemed, live), Ascending).Content
	}

	for name, content := range rendered {
		T.Run(name, func(t *testing.T) {
			t.Parallel()

			test.StrContains(t, content, "redeemed_at IS NULL", test.Sprintf("statement: %s", content))
			test.StrContains(t, content, "expires_at > ", test.Sprintf("statement: %s", content))
		})
	}
}

func TestComparand_GuardedUpdateAssignsWhatItGuardsOn(T *testing.T) {
	T.Parallel()

	T.Run("the column a guard names may be the column the SET assigns", func(t *testing.T) {
		t.Parallel()

		// This is the shape the two-factor verification is: stamp the proof,
		// where the proof is not there yet. It needs no Arg, because the guard
		// side binds nothing at all — which is exactly what makes it safe to
		// name the same column on both sides of the statement.
		got := For(dialect.Postgres).UpdateQuery("MarkVerified", guardTable, guardColumns(),
			[]string{"redeemed_at"}, []string{"redeemed_at"},
			Match{Column: "secret", Against: EmptyString, Exclude: true},
			Match{Column: "redeemed_at", Against: NoValue})

		test.StrContains(t, got.Content, "redeemed_at = sqlc.narg(redeemed_at)")
		test.StrContains(t, got.Content, "secret <> ''")
		test.StrContains(t, got.Content, "redeemed_at IS NULL")

		// One reference to the column's argument, so the assignment and the
		// guard are not two ends of one binding.
		test.EqOp(t, 1, strings.Count(got.Content, "sqlc.narg(redeemed_at)"))
	})
}

func TestComparand_ArgumentlessMatchPanics(T *testing.T) {
	T.Parallel()

	for name, comparand := range map[string]Comparand{
		"NULL":             NoValue,
		"the empty string": EmptyString,
		"the current time": CurrentTime,
	} {
		T.Run("naming an argument beside "+name, func(t *testing.T) {
			t.Parallel()

			// Ignoring the field would leave a caller with a name no marker
			// carries, which their argument map can hold forever without
			// anything reporting it.
			err := recovered(func() {
				_ = For(dialect.Postgres).matchPredicate(guardTable,
					Match{Column: "redeemed_at", Against: comparand, Arg: "whenever"}, true)
			})

			must.ErrorIs(t, err, ErrArgumentlessMatch)
			test.StrContains(t, err.Error(), "whenever")
		})
	}

	T.Run("the three comparands that bind take an argument name", func(t *testing.T) {
		t.Parallel()

		for _, comparand := range []Comparand{BoundArgument, BoundTime, OptionalArgument} {
			got := guardPredicate(t, dialect.Postgres,
				Match{Column: IDColumn, Against: comparand, Arg: "except_id"})

			test.StrContains(t, got, "except_id", test.Sprintf("comparand %q", comparand))
		}
	})
}

func TestComparand_String(T *testing.T) {
	T.Parallel()

	T.Run("names every comparand it can", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, "bound argument", BoundArgument.String())
		test.EqOp(t, "NULL", NoValue.String())
		test.EqOp(t, "the empty string", EmptyString.String())
		test.EqOp(t, "the current time", CurrentTime.String())
		test.EqOp(t, "a bound time", BoundTime.String())
		test.EqOp(t, "an optional bound argument", OptionalArgument.String())
		test.StrContains(t, Comparand(99).String(), "unknown")
	})
}
