package passwordreset

import (
	"fmt"
	"time"

	"github.com/primandproper/platform-go/v13/database/ddl"
	"github.com/primandproper/platform-go/v13/database/dialect"
	"github.com/primandproper/platform-go/v13/tenancy"
)

// resetColumns is the projection every read scans, ordered to match the Scan.
//
// token_digest is not in it, and its absence is the point: nothing in this
// package ever reads the column back. It is bound in the insert and compared
// against in the lookup, and a projection that included it would be a
// projection that put a stored credential's digest in whatever a caller did
// next with the row.
const resetColumns = "id, scope, belongs_to_user, expires_at, redeemed_at, created_at"

// A note on timestamps, because one dialect does something surprising.
//
// Every time this package binds is a UTC time.Time. Postgres and MySQL store
// these as real temporal types; SQLite does not — modernc's driver stores a
// bound time.Time as Go's own String() rendering, so the sweeper's
// `expires_at <= ?` there is a string comparison.
//
// That is still correct, because the rendering begins with a fixed-width
// "YYYY-MM-DD HH:MM:SS" prefix and everything is UTC, so lexical order is
// chronological order. It stops being correct the moment a value is bound in a
// non-UTC location, so do not remove the .UTC() calls at the binding sites.
//
// It is also why the liveness comparison is made in Go rather than added to the
// lookup's WHERE clause. The sweep can afford a lexical comparison because it
// deletes rows that are dead by any reading; the boundary a user hits at the
// last second of a link's life is worth deciding against one clock, in one
// place, on all three engines.

// tableName renders the token table's name for a namespace. The password_reset
// segment is the schema's own, so a table says which package created it even in
// a database shared between applications.
func tableName(prefix string) string {
	return ddl.Qualify(prefix) + "password_reset_tokens"
}

// buildInsert renders the issuance write.
//
// It is a plain INSERT rather than an upsert, unlike the ceremony store's
// equivalent, and the difference is what the key means. A challenge is a
// ceremony's name and beginning it twice replaces it; a digest is a secret, and
// a second row bearing one would mean the generator produced the same token
// twice. The unique index refuses that, and this statement lets it: an issuance
// failing loudly is the correct outcome of randomness that has stopped being
// random.
func buildInsert(d dialect.Dialect, table string, token *Token, digest string) (query string, args []any) {
	args = []any{
		token.ID,
		token.Scope,
		token.UserID,
		digest,
		token.ExpiresAt.UTC(),
		token.CreatedAt.UTC(),
	}

	return fmt.Sprintf(
		"INSERT INTO %s (id, scope, belongs_to_user, token_digest, expires_at, created_at) VALUES (%s)",
		table, d.Placeholders(1, len(args)),
	), args
}

// buildSelectByDigest renders the lookup Verify and Consume both begin with.
//
// The scope is a predicate rather than a check made on the row afterwards, so a
// token presented in the wrong tenant matches nothing and reads as absent —
// which is what it is from there. The Scope binds itself rather than a string
// derived from it, so an unset one is a driver error instead of a wider result
// set.
func buildSelectByDigest(d dialect.Dialect, table, digest string, scope tenancy.Scope) (query string, args []any) {
	return fmt.Sprintf(
		"SELECT %s FROM %s WHERE token_digest = %s AND scope = %s",
		resetColumns, table, d.Placeholder(1), d.Placeholder(2),
	), []any{digest, scope}
}

// buildRedeem renders the write that spends a token, and it is the statement
// that decides single use.
//
// The guard is redeemed_at IS NULL rather than an equality against what the
// read saw, because "has not happened yet" is not a value a caller holds. Two
// requests that both read the row live both reach this statement; the first
// one's update matches, the second one's finds redeemed_at already set and
// reports no rows. The count is the answer, which is why the read cannot be.
func buildRedeem(d dialect.Dialect, table, tokenID string, at time.Time) (query string, args []any) {
	return fmt.Sprintf(
		"UPDATE %s SET redeemed_at = %s WHERE id = %s AND redeemed_at IS NULL",
		table, d.Placeholder(1), d.Placeholder(2),
	), []any{at.UTC(), tokenID}
}

// buildRevokeForUser renders the destruction of one principal's outstanding
// tokens.
//
// Redeemed rows are excluded rather than swept up with the rest. They are
// already unspendable, and they are what answers "this link has already been
// used" for the rest of the link's life — a revocation that removed them would
// turn a completed reset's own token into one that never existed.
func buildRevokeForUser(d dialect.Dialect, table string, scope tenancy.Scope, userID string) (query string, args []any) {
	return fmt.Sprintf(
		"DELETE FROM %s WHERE scope = %s AND belongs_to_user = %s AND redeemed_at IS NULL",
		table, d.Placeholder(1), d.Placeholder(2),
	), []any{scope, userID}
}

// buildSweep renders the removal of every row whose deadline has passed.
//
// Redeemed rows go with them, at their own expiry rather than at their
// redemption, and that is the whole retention policy: a spent link keeps
// answering "already used" for exactly as long as it would have kept working.
// Deleting on redemption instead would make the most common reason a link fails
// indistinguishable from a link nobody ever issued.
func buildSweep(d dialect.Dialect, table string, now time.Time) (query string, args []any) {
	return fmt.Sprintf("DELETE FROM %s WHERE expires_at <= %s", table, d.Placeholder(1)),
		[]any{now.UTC()}
}
