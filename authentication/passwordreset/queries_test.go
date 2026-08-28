package passwordreset

import (
	"strings"
	"testing"
	"time"

	"github.com/primandproper/platform-go/v13/database/dialect"
	"github.com/primandproper/platform-go/v13/tenancy"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// dialects is every dialect the rendered statements have to be legal in. Each
// assertion below runs against all three, because the differences between them
// are exactly where a hand-rendered statement goes wrong.
var dialects = []dialect.Dialect{dialect.Postgres, dialect.MySQL, dialect.SQLite}

func TestTableName(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, "password_reset_tokens", tableName(DefaultTablePrefix))
		test.EqOp(t, "ddb_password_reset_tokens", tableName("ddb"))
	})
}

func TestBuildInsert(T *testing.T) {
	T.Parallel()

	token := &Token{
		ID:        "token_01",
		Scope:     testScope(),
		UserID:    testUserID,
		CreatedAt: time.Date(2026, time.August, 28, 12, 0, 0, 0, time.UTC),
		ExpiresAt: time.Date(2026, time.August, 28, 13, 0, 0, 0, time.UTC),
	}

	T.Run("binds the digest and never the token", func(t *testing.T) {
		t.Parallel()

		for _, d := range dialects {
			query, args := buildInsert(d, "password_reset_tokens", token, "digest-value")

			test.StrContains(t, query, "INSERT INTO password_reset_tokens")
			test.SliceLen(t, 6, args)
			test.EqOp(t, "digest-value", args[3])
			test.Eq(t, any(token.Scope), args[1])
			test.StrNotContains(t, query, "redeemed_at")
		}
	})

	T.Run("numbers its placeholders per dialect", func(t *testing.T) {
		t.Parallel()

		pg, _ := buildInsert(dialect.Postgres, "password_reset_tokens", token, "d")
		test.StrContains(t, pg, "VALUES ($1, $2, $3, $4, $5, $6)")

		my, _ := buildInsert(dialect.MySQL, "password_reset_tokens", token, "d")
		test.StrContains(t, my, "VALUES (?, ?, ?, ?, ?, ?)")
	})

	// A time bound in another location would compare as a different string on
	// SQLite, where the driver stores Go's own rendering.
	T.Run("binds every time in UTC", func(t *testing.T) {
		t.Parallel()

		elsewhere := time.FixedZone("UTC+7", 7*60*60)
		local := &Token{
			ID:        token.ID,
			Scope:     token.Scope,
			UserID:    token.UserID,
			CreatedAt: token.CreatedAt.In(elsewhere),
			ExpiresAt: token.ExpiresAt.In(elsewhere),
		}

		_, args := buildInsert(dialect.SQLite, "password_reset_tokens", local, "d")

		expires, ok := args[4].(time.Time)
		must.True(t, ok)
		test.EqOp(t, time.UTC, expires.Location())

		created, ok := args[5].(time.Time)
		must.True(t, ok)
		test.EqOp(t, time.UTC, created.Location())
	})
}

func TestBuildSelectByDigest(T *testing.T) {
	T.Parallel()

	// The read that a missing scope predicate would widen into every tenant's
	// tokens.
	T.Run("filters on the scope and binds the scope itself", func(t *testing.T) {
		t.Parallel()

		for _, d := range dialects {
			query, args := buildSelectByDigest(d, "password_reset_tokens", "digest-value", testScope())

			test.StrContains(t, query, "WHERE token_digest = ")
			test.StrContains(t, query, "AND scope = ")
			test.SliceLen(t, 2, args)
			test.EqOp(t, "digest-value", args[0])

			scope, ok := args[1].(tenancy.Scope)
			must.True(t, ok, must.Sprint("the scope is bound as a Scope, not as a string derived from one"))
			test.EqOp(t, testScope(), scope)
		}
	})

	// The digest is what the caller presents; the column is never handed back.
	T.Run("projects no digest", func(t *testing.T) {
		t.Parallel()

		query, _ := buildSelectByDigest(dialect.Postgres, "password_reset_tokens", "d", testScope())

		selectClause, _, found := strings.Cut(query, " FROM ")
		must.True(t, found)
		test.StrNotContains(t, selectClause, "token_digest")
	})
}

func TestBuildRedeem(T *testing.T) {
	T.Parallel()

	// The guard is what makes single use a property of the statement rather than
	// of whoever read the row first.
	T.Run("guards on the redemption not having happened", func(t *testing.T) {
		t.Parallel()

		at := time.Date(2026, time.August, 28, 12, 30, 0, 0, time.UTC)

		for _, d := range dialects {
			query, args := buildRedeem(d, "password_reset_tokens", "token_01", at)

			test.StrContains(t, query, "UPDATE password_reset_tokens SET redeemed_at = ")
			test.StrContains(t, query, "AND redeemed_at IS NULL")
			test.SliceLen(t, 2, args)
			test.Eq(t, any(at), args[0])
			test.EqOp(t, "token_01", args[1])
		}
	})
}

func TestBuildRevokeForUser(T *testing.T) {
	T.Parallel()

	T.Run("names both the scope and the user, and spares redeemed rows", func(t *testing.T) {
		t.Parallel()

		for _, d := range dialects {
			query, args := buildRevokeForUser(d, "password_reset_tokens", testScope(), testUserID)

			test.StrContains(t, query, "DELETE FROM password_reset_tokens")
			test.StrContains(t, query, "WHERE scope = ")
			test.StrContains(t, query, "AND belongs_to_user = ")
			test.StrContains(t, query, "AND redeemed_at IS NULL")
			test.SliceLen(t, 2, args)
			test.EqOp(t, testUserID, args[1])
		}
	})
}

func TestBuildSweep(T *testing.T) {
	T.Parallel()

	T.Run("deletes by deadline alone", func(t *testing.T) {
		t.Parallel()

		now := time.Date(2026, time.August, 28, 12, 0, 0, 0, time.UTC)

		for _, d := range dialects {
			query, args := buildSweep(d, "password_reset_tokens", now)

			test.StrContains(t, query, "DELETE FROM password_reset_tokens WHERE expires_at <= ")
			test.SliceLen(t, 1, args)
			test.Eq(t, any(now), args[0])
		}
	})

	T.Run("binds its deadline in UTC", func(t *testing.T) {
		t.Parallel()

		elsewhere := time.Date(2026, time.August, 28, 19, 0, 0, 0, time.FixedZone("UTC+7", 7*60*60))

		_, args := buildSweep(dialect.SQLite, "password_reset_tokens", elsewhere)

		bound, ok := args[0].(time.Time)
		must.True(t, ok)
		test.EqOp(t, time.UTC, bound.Location())
	})
}
