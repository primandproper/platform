package identity

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/primandproper/platform-go/v13/database"
	"github.com/primandproper/platform-go/v13/filtering"
	"github.com/primandproper/platform-go/v13/identifiers"
	"github.com/primandproper/platform-go/v13/pointer"
	"github.com/primandproper/platform-go/v13/tenancy"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// runDirectoryReaderSuite covers the reads: users, accounts, and the
// memberships between them, every one of them scoped.
func runDirectoryReaderSuite(t *testing.T, env *storeEnv) {
	t.Helper()

	t.Run("hides a user from another directory", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		user := createUser(t, store, newUser("ada"))

		_, err := store.GetUser(t.Context(), otherScope, user.ID)
		must.ErrorIs(t, err, ErrUserNotFound)
	})

	t.Run("refuses an unset scope", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		_, err := store.GetUser(t.Context(), tenancy.Scope{}, "whatever")
		must.ErrorIs(t, err, tenancy.ErrNoScope)
	})

	t.Run("pages the directory, redacted", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		// Written with ascending ids, because the page is ordered by id — the
		// cursor names a position in an order, and the order is the statement's.
		var created []*User
		for i, name := range []string{"ada", "brian", "carol", "dennis"} {
			user := newUser(name)
			user.ID = fmt.Sprintf("u_%02d", i)
			created = append(created, createUser(t, store, user))
		}

		neighbor := newUser("eve")
		neighbor.Scope = otherScope
		createUser(t, store, neighbor)

		page, err := store.ListUsers(t.Context(), testScope, &filtering.QueryFilter{
			MaxResponseSize: pointer.To(uint16(2)),
		})
		must.NoError(t, err)
		must.SliceLen(t, 2, page.Data)
		test.EqOp(t, "ada", page.Data[0].Username)
		test.EqOp(t, "brian", page.Data[1].Username)

		// A page is the read most likely to reach a response body.
		for _, user := range page.Data {
			test.EqOp(t, "", user.HashedPassword)
			test.EqOp(t, "", user.TwoFactorSecret)
		}

		// Both counts ride on the rows of the one statement, so the first page
		// already knows how many there are.
		filtered, total, known := page.Counts()
		test.True(t, known)
		test.EqOp(t, uint64(4), total)
		test.EqOp(t, uint64(4), filtered)

		next, err := store.ListUsers(t.Context(), testScope, &filtering.QueryFilter{
			MaxResponseSize: pointer.To(uint16(2)),
			Cursor:          pointer.To(created[1].ID),
		})
		must.NoError(t, err)
		must.SliceLen(t, 2, next.Data)
		test.EqOp(t, "carol", next.Data[0].Username)

		// The neighbor's directory is in neither count.
		filtered, total, known = next.Counts()
		test.True(t, known)
		test.EqOp(t, uint64(4), total)
		test.EqOp(t, uint64(4), filtered)
	})

	// The window the hand-written page could not express. It is the reason the
	// list signatures moved to a filter rather than a cursor and a limit.
	t.Run("pages the directory through the created window", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		early := createUser(t, store, newUser("ada"))

		// A whole second, not a millisecond: SQLite has no timestamp type, so
		// its created_at holds the text CURRENT_TIMESTAMP wrote and compares
		// lexicographically at second granularity — a sub-second cutoff there
		// is the same string as the row's own stamp and excludes it.
		cutoff := early.CreatedAt.Add(time.Second)

		before, err := store.ListUsers(t.Context(), testScope, &filtering.QueryFilter{
			CreatedBefore: pointer.To(cutoff),
		})
		must.NoError(t, err)
		must.SliceLen(t, 1, before.Data)

		after, err := store.ListUsers(t.Context(), testScope, &filtering.QueryFilter{
			CreatedAfter: pointer.To(cutoff),
		})
		must.NoError(t, err)
		test.SliceEmpty(t, after.Data)

		// An empty page carries no counts, because the counts ride on the rows
		// — and reporting the resulting zero would be indistinguishable from
		// "nothing matched".
		_, _, known := after.Counts()
		test.False(t, known)
	})

	t.Run("searches by username prefix", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		for _, name := range []string{"ada", "adam", "brian", "a%wildcard"} {
			createUser(t, store, newUser(name))
		}

		page, err := store.SearchUsersByUsername(t.Context(), testScope, "ad", nil)
		must.NoError(t, err)
		must.SliceLen(t, 2, page.Data)

		// The escape is what keeps the typed % a character: without it the
		// pattern widens past the prefix and takes ada and adam with it, which
		// reads as a working search returning too much rather than as a bug.
		wildcard, err := store.SearchUsersByUsername(t.Context(), testScope, "a%", nil)
		must.NoError(t, err)
		must.SliceLen(t, 1, wildcard.Data)
		test.EqOp(t, "a%wildcard", wildcard.Data[0].Username)
	})

	t.Run("pages a username search by the username", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		for _, name := range []string{"ada", "adam", "adele", "brian"} {
			createUser(t, store, newUser(name))
		}

		// Archived after it was written, so what the search leaves out is a
		// user the prefix matched rather than one it never did.
		gone := createUser(t, store, newUser("adrian"))
		must.NoError(t, store.ArchiveUser(t.Context(), testScope, gone.ID))

		first, err := store.SearchUsersByUsername(t.Context(), testScope, "ad",
			&filtering.QueryFilter{MaxResponseSize: pointer.To(uint16(2))})
		must.NoError(t, err)
		must.SliceLen(t, 2, first.Data)
		test.EqOp(t, "ada", first.Data[0].Username)
		test.EqOp(t, "adam", first.Data[1].Username)

		// The count is its own statement rather than one riding on the rows, so
		// it describes everything the prefix matched rather than what is left
		// after the cursor — and a partial page still reports it.
		_, total, known := first.Counts()
		must.True(t, known)
		test.EqOp(t, uint64(3), total)

		// The cursor is the username, because that is what the statement
		// ordered by. A cursor naming a position in an order the query does not
		// use is a page that skips rows and repeats others.
		test.EqOp(t, "adam", first.Cursor)

		second, err := store.SearchUsersByUsername(t.Context(), testScope, "ad",
			&filtering.QueryFilter{MaxResponseSize: pointer.To(uint16(2)), Cursor: pointer.To(first.Cursor)})
		must.NoError(t, err)
		must.SliceLen(t, 1, second.Data)
		test.EqOp(t, "adele", second.Data[0].Username)

		_, totalAgain, known := second.Counts()
		must.True(t, known)
		test.EqOp(t, total, totalAgain)
	})

	t.Run("reads a batch by ID", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		ada := createUser(t, store, newUser("ada"))
		brian := createUser(t, store, newUser("brian"))

		neighbor := newUser("eve")
		neighbor.Scope = otherScope
		createUser(t, store, neighbor)

		users, err := store.ListUsersByIDs(t.Context(), testScope,
			[]string{ada.ID, brian.ID, neighbor.ID, identifiers.New()})
		must.NoError(t, err)

		// The neighbor and the unknown ID are skipped rather than failing the
		// batch: the caller is hydrating references, and one missing author
		// should not empty the page.
		must.SliceLen(t, 2, users)

		for _, user := range users {
			test.EqOp(t, "", user.HashedPassword)
		}

		empty, err := store.ListUsersByIDs(t.Context(), testScope, nil)
		must.NoError(t, err)
		test.SliceEmpty(t, empty)
	})

	t.Run("hides an account from another directory", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		owner := createUser(t, store, newUser("ada"))
		account := createAccountFor(t, store, owner, "Acme")

		_, err := store.GetAccount(t.Context(), otherScope, account.ID)
		must.ErrorIs(t, err, ErrAccountNotFound)
	})

	t.Run("pages accounts and a user's own", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		owner := createUser(t, store, newUser("ada"))
		member := createUser(t, store, newUser("brian"))

		first := createAccountFor(t, store, owner, "First")
		second := createAccountFor(t, store, owner, "Second")

		must.NoError(t, inTransaction(t, store, func(ctx context.Context, q database.Tx) error {
			return store.CreateMembership(ctx, q, &Membership{
				Scope:            testScope,
				BelongsToUser:    member.ID,
				BelongsToAccount: second.ID,
				Roles:            []string{"account_member"},
			})
		}))

		all, err := store.ListAccounts(t.Context(), testScope, nil)
		must.NoError(t, err)
		must.SliceLen(t, 2, all.Data)

		mine, err := store.ListAccountsForUser(t.Context(), testScope, member.ID, nil)
		must.NoError(t, err)
		must.SliceLen(t, 1, mine.Data)
		test.EqOp(t, second.ID, mine.Data[0].ID)

		theirs, err := store.ListAccountsForUser(t.Context(), testScope, owner.ID, nil)
		must.NoError(t, err)
		must.SliceLen(t, 2, theirs.Data)

		_ = first

		page, err := store.ListAccounts(t.Context(), testScope, &filtering.QueryFilter{
			MaxResponseSize: pointer.To(uint16(1)),
		})
		must.NoError(t, err)
		must.SliceLen(t, 1, page.Data)
	})

	t.Run("pages a roster with redacted users", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		owner := createUser(t, store, newUser("ada"))
		account := createAccountFor(t, store, owner, "Acme")

		for _, name := range []string{"brian", "carol", "dennis"} {
			registerInto(t, store, newUser(name), account.ID)
		}

		roster, err := store.ListAccountMembers(t.Context(), testScope, account.ID, nil)
		must.NoError(t, err)
		must.SliceLen(t, 4, roster.Data)

		for _, member := range roster.Data {
			must.NotNil(t, member.User)
			test.EqOp(t, "", member.User.HashedPassword)
			test.EqOp(t, "", member.User.TwoFactorSecret)
			test.SliceNotEmpty(t, member.Roles)
			test.EqOp(t, member.BelongsToUser, member.User.ID)
		}
	})
}
