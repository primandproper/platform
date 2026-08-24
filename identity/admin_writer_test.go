package identity

import (
	"context"
	"testing"

	"github.com/primandproper/platform-go/v13/database"
	platformerrors "github.com/primandproper/platform-go/v13/errors"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// runAdminWriterSuite covers the operator's half, whose exposure through an
// ordinary request handler is a privilege escalation.
func runAdminWriterSuite(t *testing.T, env *storeEnv) {
	t.Helper()

	t.Run("moves a user between statuses", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		user := newUser("ada")
		user.AccountStatus = StatusUnverified
		createUser(t, store, user)

		must.NoError(t, store.UpdateUserAccountStatus(t.Context(), testScope, user.ID, StatusBanned, "spam"))

		banned, err := store.GetUser(t.Context(), testScope, user.ID)
		must.NoError(t, err)
		test.EqOp(t, StatusBanned, banned.AccountStatus)
		test.EqOp(t, "spam", banned.AccountStatusExplanation)
		test.False(t, banned.AccountStatus.AdmitsSignIn())

		err = store.UpdateUserAccountStatus(t.Context(), testScope, user.ID, AccountStatus("nonsense"), "")
		must.ErrorIs(t, err, platformerrors.ErrUnrecognizedInputValue)
	})

	t.Run("carries service roles on every read", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		user := newUser("ada")
		user.ServiceRoles = []string{"service_user", "service_admin"}
		createUser(t, store, user)

		byID, err := store.GetUser(t.Context(), testScope, user.ID)
		must.NoError(t, err)
		test.Eq(t, []string{"service_admin", "service_user"}, byID.ServiceRoles)

		// The same set on the sign-in read, so an admin check does not need a
		// second query and cannot be skipped by forgetting one.
		byName, err := store.GetUserByUsername(t.Context(), testScope, "ada")
		must.NoError(t, err)
		test.Eq(t, []string{"service_admin", "service_user"}, byName.ServiceRoles)

		must.NoError(t, store.SetUserServiceRoles(t.Context(), testScope, user.ID, []string{"service_user"}))

		revoked, err := store.GetUser(t.Context(), testScope, user.ID)
		must.NoError(t, err)
		test.Eq(t, []string{"service_user"}, revoked.ServiceRoles)

		// Replacing with nothing is how operator access is withdrawn.
		must.NoError(t, store.SetUserServiceRoles(t.Context(), testScope, user.ID, nil))

		cleared, err := store.GetUser(t.Context(), testScope, user.ID)
		must.NoError(t, err)
		test.SliceEmpty(t, cleared.ServiceRoles)
	})

	t.Run("refuses service roles for a user in another directory", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		user := createUser(t, store, newUser("ada"))

		err := store.SetUserServiceRoles(t.Context(), otherScope, user.ID, []string{"service_admin"})
		must.ErrorIs(t, err, ErrUserNotFound)
	})

	t.Run("refuses an empty service role name", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		user := newUser("ada")
		user.ServiceRoles = []string{"service_admin"}
		createUser(t, store, user)

		// An empty role name is a row nothing matches on, so the grant reads as
		// applied and resolves to nothing at the next permission check.
		must.ErrorIs(t,
			store.SetUserServiceRoles(t.Context(), testScope, user.ID, []string{"service_user", ""}),
			platformerrors.ErrEmptyInputParameter,
		)

		// The refusal is ahead of the write, so the roles they had are intact.
		read, err := store.GetUser(t.Context(), testScope, user.ID)
		must.NoError(t, err)
		test.Eq(t, []string{"service_admin"}, read.ServiceRoles)
	})

	t.Run("ends memberships when a user is archived", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		owner := createUser(t, store, newUser("ada"))
		account := createAccountFor(t, store, owner, "Acme")

		member := registerInto(t, store, newUser("brian"), account.ID)

		must.NoError(t, store.ArchiveUser(t.Context(), testScope, member.ID))

		// A user archived with live memberships is still on the rosters of the
		// accounts they belonged to, which is what an application discovers when
		// a deleted colleague is still listed.
		_, err := store.GetMembership(t.Context(), testScope, member.ID, account.ID)
		must.ErrorIs(t, err, ErrMembershipNotFound)

		// Archiving twice must not move the timestamp and lose when it first
		// happened.
		must.ErrorIs(t, store.ArchiveUser(t.Context(), testScope, member.ID), ErrUserNotFound)
	})

	t.Run("erases a user and everything keyed to them", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		owner := createUser(t, store, newUser("ada"))
		account := createAccountFor(t, store, owner, "Acme")
		member := registerInto(t, store, newUser("brian"), account.ID)

		var erased int64

		must.NoError(t, inTransaction(t, store, func(ctx context.Context, q database.Tx) error {
			var err error
			erased, err = store.EraseUser(ctx, q, testScope, member.ID)

			return err
		}))
		test.EqOp(t, int64(1), erased)

		_, err := store.GetUser(t.Context(), testScope, member.ID)
		must.ErrorIs(t, err, ErrUserNotFound)

		// The membership went with the row, through the schema's cascade — an
		// erasure that left one behind would leave the subject's account list
		// intact.
		roster, err := store.ListAccountMembers(t.Context(), testScope, account.ID, nil)
		must.NoError(t, err)
		must.SliceLen(t, 1, roster.Data)
		test.EqOp(t, owner.ID, roster.Data[0].BelongsToUser)

		// The handle is free again, which a soft delete deliberately does not do.
		createUser(t, store, newUser("brian"))
	})

	t.Run("ends memberships when an account is archived", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		owner := createUser(t, store, newUser("ada"))
		account := createAccountFor(t, store, owner, "Acme")
		member := registerInto(t, store, newUser("brian"), account.ID)

		must.NoError(t, store.ArchiveAccount(t.Context(), testScope, account.ID))

		// The row is still readable, as an archived user's is: an invoice and an
		// audit entry both still name it.
		read, err := store.GetAccount(t.Context(), testScope, account.ID)
		must.NoError(t, err)
		test.True(t, read.Archived())

		// It is out of the directory's page, though.
		page, err := store.ListAccounts(t.Context(), testScope, nil)
		must.NoError(t, err)
		test.SliceEmpty(t, page.Data)

		// Members left live against an archived account keep it in their
		// switcher and keep resolving permissions through it.
		_, err = store.GetMembership(t.Context(), testScope, member.ID, account.ID)
		must.ErrorIs(t, err, ErrMembershipNotFound)

		mine, err := store.ListAccountsForUser(t.Context(), testScope, member.ID, nil)
		must.NoError(t, err)
		test.SliceEmpty(t, mine.Data)

		must.ErrorIs(t, store.ArchiveAccount(t.Context(), testScope, account.ID), ErrAccountNotFound)
	})
}
