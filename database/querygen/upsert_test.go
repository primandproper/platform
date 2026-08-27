package querygen

import (
	"errors"
	"strings"
	"testing"

	"github.com/primandproper/platform-go/v13/database/dialect"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// membershipColumns is the table this package's first upsert consumer keys on a
// pair rather than on its id — identity's memberships, spelled here so the
// assertions read against a real shape rather than a contrived one.
var membershipColumns = []string{
	IDColumn,
	"scope",
	"belongs_to_user",
	BelongsToAccountColumn,
	"default_account",
	CreatedAtColumn,
	LastUpdatedAtColumn,
	ArchivedAtColumn,
}

// membershipKey is the unique index the conflict is detected on.
var membershipKey = []Match{{Column: "belongs_to_user"}, {Column: BelongsToAccountColumn}}

// upsertFor renders the membership upsert for one dialect, which is what almost
// every assertion below needs.
func upsertFor(d dialect.Dialect) string {
	return For(d).UpsertQuery("UpsertMembership", "memberships",
		membershipColumns,
		ForInsert(membershipColumns),
		[]string{"default_account"},
		nil,
		membershipKey...,
	).Content
}

func TestUpsertQuery(T *testing.T) {
	T.Parallel()

	T.Run("annotates the statement as an exec", func(t *testing.T) {
		t.Parallel()

		query := For(dialect.Postgres).UpsertQuery("UpsertMembership", "memberships",
			membershipColumns, ForInsert(membershipColumns), []string{"default_account"}, nil, membershipKey...)

		test.EqOp(t, "UpsertMembership", query.Annotation.Name)
		test.EqOp(t, ExecType, query.Annotation.Type)
	})

	T.Run("inserts what the create inserts", func(t *testing.T) {
		t.Parallel()

		// The INSERT half is the create's own rendering, so the two cannot come
		// to disagree about which columns a caller supplies or which of them
		// may be NULL. Comparing the text is what pins that rather than the
		// comment claiming it.
		for _, d := range everyDialect() {
			insert, _, found := strings.Cut(upsertFor(d), "\nON ")
			must.True(t, found, must.Sprintf("no conflict branch in the %s upsert", d))

			test.EqOp(t, createStatement("memberships", ForInsert(membershipColumns), nil), insert+";")
		}
	})

	T.Run("renders the conflict target from the key on Postgres and SQLite", func(t *testing.T) {
		t.Parallel()

		for _, d := range []dialect.Dialect{dialect.Postgres, dialect.SQLite} {
			statement := upsertFor(d)

			test.StrContains(t, statement,
				"ON CONFLICT (belongs_to_user, belongs_to_account) DO UPDATE SET")
			test.StrContains(t, statement, "default_account = EXCLUDED.default_account")
		}
	})

	T.Run("renders MySQL's targetless clause and its VALUES alias", func(t *testing.T) {
		t.Parallel()

		statement := upsertFor(dialect.MySQL)

		// MySQL has nowhere to put a conflict target: the clause fires on
		// whichever unique key was violated.
		test.StrContains(t, statement, "ON DUPLICATE KEY UPDATE")
		test.StrNotContains(t, statement, "ON CONFLICT")
		test.StrContains(t, statement, "default_account = VALUES(default_account)")
		test.StrNotContains(t, statement, "EXCLUDED")
	})

	T.Run("revives an archived row and stamps the update", func(t *testing.T) {
		t.Parallel()

		for _, d := range everyDialect() {
			statement := upsertFor(d)

			// Leaving archived_at set would be a write that reports success
			// and leaves the row invisible to every read.
			test.StrContains(t, statement, "archived_at = NULL")
			test.StrContains(t, statement, "last_updated_at = "+For(d).storedNow())
		}
	})

	T.Run("never assigns created_at", func(t *testing.T) {
		t.Parallel()

		// Once, in neither the insert nor the conflict branch: rejoining does
		// not make the relationship new, and the database owns the column.
		for _, d := range everyDialect() {
			test.EqOp(t, 0, strings.Count(upsertFor(d), CreatedAtColumn))
		}
	})

	T.Run("takes exactly the create's arguments", func(t *testing.T) {
		t.Parallel()

		// Every assignment in the conflict branch reads a value the INSERT
		// already carried, so an upsert binds nothing the create does not.
		for _, d := range everyDialect() {
			_, args := bindArguments(d, upsertFor(d))

			test.Eq(t, ForInsert(membershipColumns), args)
		}
	})

	T.Run("drops a key column from the assignments", func(t *testing.T) {
		t.Parallel()

		// On MySQL the collision may have been on some other unique key, and
		// assigning a key column would move the row onto the incoming key
		// rather than restate it.
		for _, d := range everyDialect() {
			statement := For(d).UpsertQuery("UpsertMembership", "memberships",
				membershipColumns,
				ForInsert(membershipColumns),
				[]string{"belongs_to_user", BelongsToAccountColumn, "default_account"},
				nil,
				membershipKey...,
			).Content

			_, branch, _ := strings.Cut(statement, "\nON ")

			test.StrNotContains(t, branch, "belongs_to_user =")
			test.StrNotContains(t, branch, "belongs_to_account =")
			test.StrContains(t, branch, "default_account =")
		}
	})

	T.Run("binds a nullable inserted column through narg", func(t *testing.T) {
		t.Parallel()

		columns := []string{IDColumn, "subject", "note", LastUpdatedAtColumn}

		statement := For(dialect.Postgres).UpsertQuery("UpsertThing", "things",
			columns, ForInsert(columns), []string{"note"}, []string{"note"},
			Match{Column: "subject"},
		).Content

		test.StrContains(t, statement, "sqlc.narg(note)")
		test.StrContains(t, statement, "note = EXCLUDED.note")

		// No archived_at column, so no revival — the table does not soft-delete.
		test.StrNotContains(t, statement, "archived_at")
	})

	T.Run("rejects an upsert with no conflict target", func(t *testing.T) {
		t.Parallel()

		// Postgres and SQLite would not parse it and MySQL would take any
		// unique key at all, so there is nothing useful to emit.
		err := recovered(func() {
			For(dialect.Postgres).UpsertQuery("UpsertMembership", "memberships",
				membershipColumns, ForInsert(membershipColumns), []string{"default_account"}, nil)
		})

		must.Error(t, err)
		test.True(t, errors.Is(err, ErrDegenerateUpsert))
		test.StrContains(t, err.Error(), "names no conflict target")
	})

	T.Run("rejects an upsert that inserts nothing", func(t *testing.T) {
		t.Parallel()

		err := recovered(func() {
			For(dialect.Postgres).UpsertQuery("UpsertMembership", "memberships",
				membershipColumns, nil, []string{"default_account"}, nil, membershipKey...)
		})

		must.Error(t, err)
		test.True(t, errors.Is(err, ErrDegenerateUpsert))
		test.StrContains(t, err.Error(), "inserts no columns")
	})

	T.Run("rejects an upsert whose conflict branch assigns nothing", func(t *testing.T) {
		t.Parallel()

		// No archived_at, no last_updated_at, and the only named update column
		// is the key itself — which leaves an INSERT that fails on its second
		// call rather than a write that converges.
		columns := []string{IDColumn, "subject"}

		err := recovered(func() {
			For(dialect.Postgres).UpsertQuery("UpsertThing", "things",
				columns, ForInsert(columns), []string{"subject"}, nil, Match{Column: "subject"})
		})

		must.Error(t, err)
		test.True(t, errors.Is(err, ErrDegenerateUpsert))
		test.StrContains(t, err.Error(), "assigns nothing on conflict")
	})
}

func TestUpsertQuery_RendersOneNameThreeTexts(t *testing.T) {
	t.Parallel()

	// The point of the whole shape: one call, one query name, three statements
	// whose difference is the conflict grammar and nothing else.
	rendered := map[dialect.Dialect]string{}

	for _, d := range everyDialect() {
		query := For(d).UpsertQuery("UpsertMembership", "memberships",
			membershipColumns, ForInsert(membershipColumns), []string{"default_account"}, nil, membershipKey...)

		test.EqOp(t, "UpsertMembership", query.Annotation.Name)

		rendered[d] = query.Content
	}

	test.NotEqOp(t, rendered[dialect.Postgres], rendered[dialect.MySQL])

	// Postgres and SQLite agree here only because neither the archived stamp nor
	// the update stamp diverges on them; MySQL's does, which is storedNow.
	test.EqOp(t, rendered[dialect.Postgres], rendered[dialect.SQLite])
}
