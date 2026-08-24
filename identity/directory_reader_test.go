package identity

import (
	"context"
	"testing"

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
		for _, name := range []string{"ada", "brian", "carol", "dennis"} {
			createUser(t, store, newUser(name))
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

		next, err := store.ListUsers(t.Context(), testScope, &filtering.QueryFilter{
			MaxResponseSize: pointer.To(uint16(2)),
			Cursor:          pointer.To("brian"),
		})
		must.NoError(t, err)
		must.SliceLen(t, 2, next.Data)
		test.EqOp(t, "carol", next.Data[0].Username)

		// The neighbor's directory is not in the count.
		filtered, total, known := next.Counts()
		test.True(t, known)
		test.EqOp(t, uint64(4), total)
		test.EqOp(t, uint64(2), filtered)
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

		// The escape is what stops "a%" matching the whole directory, which
		// reads as a working search returning too much rather than as a bug.
		wildcard, err := store.SearchUsersByUsername(t.Context(), testScope, "a%", nil)
		must.NoError(t, err)
		must.SliceLen(t, 1, wildcard.Data)
		test.EqOp(t, "a%wildcard", wildcard.Data[0].Username)
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
