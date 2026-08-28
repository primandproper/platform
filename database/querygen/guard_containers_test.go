package querygen

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/primandproper/platform-go/v13/database/dialect"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// The guard comparands are the half of this package whose promise is entirely
// behavioral. A string comparison can say that `redeemed_at IS NULL` was
// rendered; only a server can say that redeeming twice writes once, that an
// expired token is not spendable, and that a sweep collects exactly the rows
// past their deadline and none of the rows at it.
//
// So the suite below runs against each of the three, from the same statements,
// and every assertion is about rows affected rather than about text. The clock
// in particular is worth running rather than reading: it is the one comparand
// the three dialects do not spell alike, and SQLite compares it as text.

// tokensDDL is the guard suite's table: something to spend, a stamp saying it
// was spent, and a deadline.
//
// The nullable stamp and the NOT NULL secret are the two shapes the comparands
// were added for — "has not happened yet" and "holds nothing yet" — and they are
// declared differently on purpose, because the empty-string sentinel exists
// precisely for the columns that cannot be NULL.
func tokensDDL(d dialect.Dialect, table string) string {
	switch d {
	case dialect.MySQL:
		return fmt.Sprintf(`CREATE TABLE %s (
			id VARCHAR(64) NOT NULL PRIMARY KEY,
			secret VARCHAR(255) NOT NULL DEFAULT '',
			redeemed_at DATETIME(6) NULL,
			expires_at DATETIME(6) NOT NULL,
			created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
			last_updated_at DATETIME(6) NULL,
			archived_at DATETIME(6) NULL
		)`, table)
	case dialect.SQLite:
		return fmt.Sprintf(`CREATE TABLE %s (
			id TEXT NOT NULL PRIMARY KEY,
			secret TEXT NOT NULL DEFAULT '',
			redeemed_at TEXT,
			expires_at TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			last_updated_at TEXT,
			archived_at TEXT
		)`, table)
	// Postgres, which For has already narrowed the alternatives to.
	default:
		return fmt.Sprintf(`CREATE TABLE %s (
			id TEXT NOT NULL PRIMARY KEY,
			secret TEXT NOT NULL DEFAULT '',
			redeemed_at TIMESTAMP WITH TIME ZONE,
			expires_at TIMESTAMP WITH TIME ZONE NOT NULL,
			created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
			last_updated_at TIMESTAMP WITH TIME ZONE,
			archived_at TIMESTAMP WITH TIME ZONE
		)`, table)
	}
}

// tokenNullable is the one column the create may leave unset: an unredeemed
// token's stamp. The secret is NOT NULL and holds the empty string instead,
// which is what the not-empty guard reads.
func tokenNullable() []string {
	return []string{"redeemed_at", LastUpdatedAtColumn}
}

// The guards, spelled once and used by every statement below. A guard and the
// sweep that collects what it refuses are the same Match with Exclude between
// them, which is the property runGuardSuite is really checking.
func unredeemed() Match { return Match{Column: "redeemed_at", Against: NoValue} }
func stillLive() Match  { return Match{Column: "expires_at", Against: CurrentTime, Exclude: true} }
func expired() Match    { return Match{Column: "expires_at", Against: CurrentTime} }
func hasSecret() Match  { return Match{Column: "secret", Against: EmptyString, Exclude: true} }

// elapsedAt is the same boundary as expired, read against an instant the caller
// binds instead of against the server's clock. It is the comparand a store
// whose deadlines were stamped by its own clock reaches for, and the only one
// that can be asked about an instant other than now.
func elapsedAt() Match {
	return Match{Column: "expires_at", Against: BoundInstant, Arg: "horizon"}
}

// tokenQueries is every statement the guard suite runs: the standard set for
// the create, and the four guarded ones beside it.
func tokenQueries(d dialect.Dialect) []*Query {
	g := For(d)

	queries := g.StandardCRUD(guardTable, guardColumns(),
		WithEntity("Token", "Tokens"),
		WithNullable(tokenNullable()...),
		WithRegistry(NewRegistry()),
	)

	return append(queries,
		// Spend a token: stamp it, where it exists, has not been spent, and has
		// not elapsed. Every conjunct but the id is a guard, so there is no
		// argument a caller could leave unset to relax any of them.
		g.UpdateQuery("RedeemToken", guardTable, guardColumns(),
			[]string{"redeemed_at"}, tokenNullable(),
			hasSecret(), unredeemed(), stillLive()),

		// The same statement without the clock, for showing that it is the
		// clock rather than the stamp that refuses an elapsed token.
		g.UpdateQuery("RedeemTokenIgnoringExpiry", guardTable, guardColumns(),
			[]string{"redeemed_at"}, tokenNullable(),
			hasSecret(), unredeemed()),

		// The sweep: every live row past its deadline, keyed on the clock and
		// nothing else. This is the shape a session revocation takes, and it is
		// the uninverted half of the guard above.
		g.ArchiveQuery("ArchiveExpiredTokens", guardTable,
			without(guardColumns(), IDColumn), expired()),

		// The same sweep against a horizon the caller names. Worth running
		// rather than reading on all three: this is the one comparand that puts
		// a Go time on one side of a comparison and a stored one on the other,
		// and SQLite compares both as text.
		g.ArchiveQuery("ArchiveTokensElapsedAt", guardTable,
			without(guardColumns(), IDColumn), elapsedAt()),

		// The collision check's shape: a read excluding a row the caller may
		// not have. It is rendered from no columns at all, so it sees archived
		// rows too — which is what a uniqueness check over an index that covers
		// them has to do.
		g.ReadQuery("GetTokenIDBySecret", guardTable, nil,
			Read{Projection: []string{IDColumn}},
			Match{Column: "secret"},
			Match{Column: IDColumn, Against: OptionalArgument, Arg: "except_id", Exclude: true}),
	)
}

// tokenQuery finds one of the statements above and returns it bound.
func tokenQuery(tb testing.TB, d dialect.Dialect, name string, values map[string]any) (statement string, arguments []any) {
	tb.Helper()

	bound, order := bindArguments(d, named(tb, tokenQueries(d), name).Content)

	return bound, argumentsFor(tb, order, values)
}

// insertToken runs the generated create.
func insertToken(tb testing.TB, ctx context.Context, d dialect.Dialect, db *sql.DB, id, secret string, expiresAt time.Time) {
	tb.Helper()

	statement, arguments := tokenQuery(tb, d, "CreateToken", map[string]any{
		IDColumn:      id,
		"secret":      secret,
		"redeemed_at": nil,
		"expires_at":  timeArg(d, expiresAt),
	})

	_, err := db.ExecContext(ctx, statement, arguments...)
	must.NoError(tb, err)
}

// affected runs a write and reports the rows it touched, which is the whole
// answer a guarded write gives its caller.
func affected(tb testing.TB, ctx context.Context, db *sql.DB, statement string, arguments []any) int64 {
	tb.Helper()

	result, err := db.ExecContext(ctx, statement, arguments...)
	must.NoError(tb, err)

	count, err := result.RowsAffected()
	must.NoError(tb, err)

	return count
}

// redeem spends one token through the named statement, reporting the rows it
// touched.
func redeem(tb testing.TB, ctx context.Context, d dialect.Dialect, db *sql.DB, name, id string, at time.Time) int64 {
	tb.Helper()

	statement, arguments := tokenQuery(tb, d, name, map[string]any{
		IDColumn:      id,
		"redeemed_at": timeArg(d, at),
	})

	return affected(tb, ctx, db, statement, arguments)
}

func runGuardSuite(t *testing.T, ctx context.Context, d dialect.Dialect, db *sql.DB) {
	t.Helper()

	_, err := db.ExecContext(ctx, tokensDDL(d, guardTable))
	must.NoError(t, err)

	t.Run("every guarded statement is one the server accepts", func(t *testing.T) {
		for _, query := range tokenQueries(d) {
			prepare(t, ctx, d, db, query)
		}
	})

	now := time.Now().UTC()

	var (
		future = now.Add(time.Hour)
		past   = now.Add(-time.Hour)
	)

	insertToken(t, ctx, d, db, "t_live", "secret_live", future)
	insertToken(t, ctx, d, db, "t_elapsed", "secret_elapsed", past)
	insertToken(t, ctx, d, db, "t_secretless", "", future)

	t.Run("the NULL guard spends a token once", func(t *testing.T) {
		// The first redemption stamps the row; the second finds the stamp it
		// wrote and matches nothing. That zero is the answer — it is what a
		// caller reads as "already spent" rather than moving the stamp forward
		// and losing when the first spend happened.
		test.EqOp(t, int64(1), redeem(t, ctx, d, db, "RedeemToken", "t_live", now))
		test.EqOp(t, int64(0), redeem(t, ctx, d, db, "RedeemToken", "t_live", now.Add(time.Minute)))
	})

	t.Run("the not-empty guard refuses a column holding nothing", func(t *testing.T) {
		// The row exists, is unredeemed and is live. What it does not have is a
		// secret, and on a NOT NULL column that is the empty string rather than
		// a NULL — so this is the conjunct no IS NULL could have expressed.
		test.EqOp(t, int64(0), redeem(t, ctx, d, db, "RedeemToken", "t_secretless", now))
	})

	t.Run("the clock guard refuses a row past its deadline", func(t *testing.T) {
		// Both statements are the same statement bar the clock conjunct, so the
		// pair isolates it: the elapsed token is refused by the guarded one and
		// accepted by the one without it. Otherwise a clock predicate that
		// compared the wrong way round, or against a truncated now, would look
		// exactly like a token that was simply already spent.
		test.EqOp(t, int64(0), redeem(t, ctx, d, db, "RedeemToken", "t_elapsed", now))
		test.EqOp(t, int64(1), redeem(t, ctx, d, db, "RedeemTokenIgnoringExpiry", "t_elapsed", now))
	})

	t.Run("the sweep collects what the guard refuses", func(t *testing.T) {
		// The uninverted clock, over every row rather than one. It must take the
		// elapsed token and leave the two live ones, which is the same boundary
		// the guard above enforces read from the other side.
		statement, arguments := tokenQuery(t, d, "ArchiveExpiredTokens", map[string]any{})

		test.EqOp(t, int64(1), affected(t, ctx, db, statement, arguments))
		test.Eq(t, []string{"t_elapsed"}, archivedTokenIDs(t, ctx, db))

		// And it is idempotent, because the archived predicate the column list
		// contributes has already excluded what the last run took.
		test.EqOp(t, int64(0), affected(t, ctx, db, statement, arguments))
	})

	t.Run("the bound horizon collects what elapsed by the instant it names", func(t *testing.T) {
		// Two rows dead by different amounts, so the horizon has something to
		// discriminate. Fresh ids, because the sweep above has already archived
		// what it took and the archived predicate excludes it.
		insertToken(t, ctx, d, db, "t_long_dead", "secret_long_dead", now.Add(-2*time.Hour))
		insertToken(t, ctx, d, db, "t_just_dead", "secret_just_dead", now.Add(-30*time.Minute))

		sweep := func(horizon time.Time) int64 {
			statement, arguments := tokenQuery(t, d, "ArchiveTokensElapsedAt", map[string]any{
				"horizon": timeArg(d, horizon),
			})

			return affected(t, ctx, db, statement, arguments)
		}

		// An hour back reaches the row that died two hours ago and leaves the
		// one that died half an hour ago — which is the whole point of a bound
		// horizon, and the thing the server's clock has no argument to say.
		test.EqOp(t, int64(1), sweep(now.Add(-time.Hour)))
		test.Eq(t, []string{"t_elapsed", "t_long_dead"}, archivedTokenIDs(t, ctx, db))

		// And at now it reaches the other one, so the two calls together
		// partition the dead rows rather than one of them taking both.
		test.EqOp(t, int64(1), sweep(now))
		test.Eq(t, []string{"t_elapsed", "t_just_dead", "t_long_dead"}, archivedTokenIDs(t, ctx, db))

		// The boundary is inclusive on the elapsed side, matching the clock's,
		// so a row whose deadline is exactly the horizon is past it.
		insertToken(t, ctx, d, db, "t_on_the_boundary", "secret_boundary", now)
		test.EqOp(t, int64(1), sweep(now))
	})

	t.Run("the optional argument excludes a row only when the caller names one", func(t *testing.T) {
		statement, arguments := tokenQuery(t, d, "GetTokenIDBySecret", map[string]any{
			"secret":    "secret_live",
			"except_id": nil,
		})

		test.Eq(t, []string{"t_live"}, scanIDs(t, ctx, db, statement, arguments))

		// The same statement, with the row itself excluded — which is what lets
		// a uniqueness check pass when the only collision is with the row being
		// updated.
		statement, arguments = tokenQuery(t, d, "GetTokenIDBySecret", map[string]any{
			"secret":    "secret_live",
			"except_id": "t_live",
		})

		test.SliceEmpty(t, scanIDs(t, ctx, db, statement, arguments))

		// And an unset argument excludes nothing rather than everything, which
		// is the failure the COALESCE exists to prevent: it coalesces to the
		// empty string, and no id is empty.
		statement, arguments = tokenQuery(t, d, "GetTokenIDBySecret", map[string]any{
			"secret":    "secret_elapsed",
			"except_id": nil,
		})

		test.Eq(t, []string{"t_elapsed"}, scanIDs(t, ctx, db, statement, arguments))
	})
}

// archivedTokenIDs reads what the sweep took, in id order.
func archivedTokenIDs(tb testing.TB, ctx context.Context, db *sql.DB) []string {
	tb.Helper()

	return scanIDs(tb, ctx, db,
		"SELECT id FROM "+guardTable+" WHERE archived_at IS NOT NULL ORDER BY id", nil)
}
