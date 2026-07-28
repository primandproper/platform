package outbox

import (
	"strings"
	"testing"
	"time"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestValidIdentifier(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		valid := []string{"outbox_messages", "_outbox", "Outbox1", "events.outbox_messages"}
		for _, s := range valid {
			test.True(t, validIdentifier(s), test.Sprintf("expected %q to be valid", s))
		}

		// Table names are interpolated, not bound, so anything that could carry
		// SQL has to be refused here.
		invalid := []string{
			"", " ", "1outbox", "out box", "outbox;DROP TABLE users", "outbox--", "a.b.c", ".outbox", "outbox.",
			`outbox"`, "outbox'", "outbox`",
		}
		for _, s := range invalid {
			test.False(t, validIdentifier(s), test.Sprintf("expected %q to be invalid", s))
		}
	})
}

func TestPlaceholders(T *testing.T) {
	T.Parallel()

	T.Run("numbers postgres placeholders and repeats the others", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, "$1", placeholder(DialectPostgres, 1))
		test.EqOp(t, "$7", placeholder(DialectPostgres, 7))
		test.EqOp(t, "?", placeholder(DialectMySQL, 3))
		test.EqOp(t, "?", placeholder(DialectSQLite, 3))

		test.EqOp(t, "$2, $3, $4", placeholderList(DialectPostgres, 2, 3))
		test.EqOp(t, "?, ?, ?", placeholderList(DialectSQLite, 2, 3))
		test.EqOp(t, "", placeholderList(DialectPostgres, 1, 0))
	})
}

func TestBuildInsert(T *testing.T) {
	T.Parallel()

	T.Run("binds every column and repeats created_at as next_attempt", func(t *testing.T) {
		t.Parallel()

		now := time.Date(2026, time.July, 27, 12, 0, 0, 0, time.UTC)
		rows := []enqueueRow{
			{id: "1", topic: "orders", key: "k", payload: []byte(`{}`), createdAt: now},
			{id: "2", topic: "shipments", payload: []byte(`{}`), createdAt: now},
		}

		query, args := buildInsert(DialectPostgres, DefaultTableName, rows)

		test.True(t, strings.Contains(query, "($1, $2, $3, $4, $5, $6)"))
		test.True(t, strings.Contains(query, "($7, $8, $9, $10, $11, $12)"))
		test.SliceLen(t, 12, args)

		// created_at is bound twice per row so a new message is eligible now.
		test.Eq(t, any(now), args[4])
		test.Eq(t, any(now), args[5])

		mysqlQuery, _ := buildInsert(DialectMySQL, DefaultTableName, rows[:1])
		test.True(t, strings.Contains(mysqlQuery, "(?, ?, ?, ?, ?, ?)"))
	})
}

func TestBuildSelectClaimable(T *testing.T) {
	T.Parallel()

	T.Run("appends SKIP LOCKED only where it is supported and requested", func(t *testing.T) {
		t.Parallel()

		now := time.Now().UTC()

		pg, args := buildSelectClaimable(DialectPostgres, DefaultTableName, now, 10, true)
		test.True(t, strings.HasSuffix(pg, "FOR UPDATE SKIP LOCKED"))
		test.SliceLen(t, 3, args)

		lease, _ := buildSelectClaimable(DialectPostgres, DefaultTableName, now, 10, false)
		test.False(t, strings.Contains(lease, "SKIP LOCKED"))

		// SQLite has no SKIP LOCKED; asking for it must not produce invalid SQL.
		lite, _ := buildSelectClaimable(DialectSQLite, DefaultTableName, now, 10, true)
		test.False(t, strings.Contains(lite, "SKIP LOCKED"))
	})

	T.Run("carries the per-key ordering predicate", func(t *testing.T) {
		t.Parallel()

		query, _ := buildSelectClaimable(DialectPostgres, DefaultTableName, time.Now().UTC(), 10, false)

		// This subquery is the whole ordering guarantee: a keyed row is
		// claimable only when nothing older with that key is still pending.
		test.True(t, strings.Contains(query, "NOT EXISTS"))
		test.True(t, strings.Contains(query, "prior.created_at < m.created_at"))
		test.True(t, strings.Contains(query, "m.partition_key = ''"))
	})
}

func TestBuildReap(T *testing.T) {
	T.Parallel()

	T.Run("wraps the subquery for MySQL only", func(t *testing.T) {
		t.Parallel()

		before := time.Now().UTC()

		mysqlQuery, args := buildReap(DialectMySQL, DefaultTableName, before, 100)
		// MySQL rejects reading the table being deleted from unless the
		// subquery is materialized.
		test.True(t, strings.Contains(mysqlQuery, "AS doomed"))
		test.SliceLen(t, 2, args)

		pgQuery, _ := buildReap(DialectPostgres, DefaultTableName, before, 100)
		test.False(t, strings.Contains(pgQuery, "AS doomed"))
	})
}

func TestBuildRecordFailure(T *testing.T) {
	T.Parallel()

	T.Run("binds the schedule, the reason, and the quarantine flag", func(t *testing.T) {
		t.Parallel()

		next := time.Now().UTC()

		query, args := buildRecordFailure(DialectPostgres, DefaultTableName, "id-1", next, "boom", true)

		test.True(t, strings.Contains(query, "claimed_until = NULL"))
		test.Eq(t, []any{next, "boom", true, "id-1"}, args)
	})
}

func TestCoerceTime(T *testing.T) {
	T.Parallel()

	T.Run("accepts every rendering a driver might return", func(t *testing.T) {
		t.Parallel()

		want := time.Date(2026, time.July, 27, 12, 0, 0, 0, time.UTC)

		cases := map[string]any{
			"time.Time":         want,
			"go String layout":  "2026-07-27 12:00:00 +0000 UTC",
			"RFC3339":           "2026-07-27T12:00:00Z",
			"space offset":      "2026-07-27 12:00:00+00:00",
			"no zone":           "2026-07-27 12:00:00",
			"bytes":             []byte("2026-07-27 12:00:00 +0000 UTC"),
			"fractional string": "2026-07-27 12:00:00.000000000 +0000 UTC",
		}

		for name, in := range cases {
			got, ok := coerceTime(in)
			must.True(t, ok, must.Sprintf("case %q", name))
			test.True(t, want.Equal(got), test.Sprintf("case %q: got %s", name, got))
		}
	})

	T.Run("reports false for a NULL or unusable value", func(t *testing.T) {
		t.Parallel()

		// A NULL is an empty backlog, not an error.
		for _, in := range []any{nil, "", "not a time", 42, []byte("nope")} {
			_, ok := coerceTime(in)
			test.False(t, ok, test.Sprintf("value %v", in))
		}
	})
}

func TestBuildBacklog(T *testing.T) {
	T.Parallel()

	T.Run("excludes published and quarantined rows", func(t *testing.T) {
		t.Parallel()

		query := buildBacklog(DefaultTableName)

		test.True(t, strings.Contains(query, "COUNT(*)"))
		test.True(t, strings.Contains(query, "MIN(created_at)"))
		test.True(t, strings.Contains(query, "published_at IS NULL"))
		test.True(t, strings.Contains(query, "quarantined = FALSE"))
	})
}

func TestTruncateError(T *testing.T) {
	T.Parallel()

	T.Run("bounds what is stored", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, "", truncateError(nil))

		long := errorOfLength(maxStoredErrorLength * 2)
		test.EqOp(t, maxStoredErrorLength, len(truncateError(long)))
	})
}

type fixedError string

func (e fixedError) Error() string { return string(e) }

func errorOfLength(n int) error {
	return fixedError(strings.Repeat("x", n))
}
