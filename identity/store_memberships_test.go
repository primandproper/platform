package identity

import (
	"context"
	"testing"

	"github.com/primandproper/platform-go/v13/database"
	platformerrors "github.com/primandproper/platform-go/v13/errors"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func runMembershipSuite(t *testing.T, env *storeEnv) {
	t.Helper()

	t.Run("makes the first membership the default", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		owner := createUser(t, store, newUser("ada"))
		first := createAccountFor(t, store, owner, "First")

		membership, err := store.GetMembership(t.Context(), testScope, owner.ID, first.ID)
		must.NoError(t, err)

		// The caller said nothing about the default, and a user with
		// memberships and none is a user with nowhere to land.
		test.True(t, membership.DefaultAccount)
		test.Eq(t, []string{"account_admin"}, membership.Roles)

		second := createAccountFor(t, store, owner, "Second")

		later, err := store.GetMembership(t.Context(), testScope, owner.ID, second.ID)
		must.NoError(t, err)
		test.False(t, later.DefaultAccount)
	})

	t.Run("keeps exactly one default", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		owner := createUser(t, store, newUser("ada"))
		first := createAccountFor(t, store, owner, "First")
		second := createAccountFor(t, store, owner, "Second")

		must.NoError(t, store.SetDefaultAccount(t.Context(), testScope, owner.ID, second.ID))

		memberships, err := store.ListMembershipsForUser(t.Context(), testScope, owner.ID)
		must.NoError(t, err)
		must.SliceLen(t, 2, memberships)

		// Default first, so a caller taking the head gets the one the user lands
		// in.
		test.EqOp(t, second.ID, memberships[0].BelongsToAccount)
		test.True(t, memberships[0].DefaultAccount)

		defaults := 0

		for _, membership := range memberships {
			if membership.DefaultAccount {
				defaults++
			}
		}

		test.EqOp(t, 1, defaults)
		test.EqOp(t, first.ID, memberships[1].BelongsToAccount)

		err = store.SetDefaultAccount(t.Context(), testScope, owner.ID, "not-an-account")
		must.ErrorIs(t, err, ErrMembershipNotFound)
	})

	t.Run("revives an archived membership rather than duplicating it", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		owner := createUser(t, store, newUser("ada"))
		account := createAccountFor(t, store, owner, "Acme")
		member := registerInto(t, store, newUser("brian"), account.ID)

		original, err := store.GetMembership(t.Context(), testScope, member.ID, account.ID)
		must.NoError(t, err)

		must.NoError(t, store.RemoveMembership(t.Context(), testScope, member.ID, account.ID))

		// Rejoining. The pair is unique across live and archived rows, so this
		// has to revive rather than insert — and it keeps the ID it was created
		// with, which is what the roles are written against.
		must.NoError(t, inTransaction(t, store, func(ctx context.Context, q database.SQLQueryExecutor) error {
			return store.CreateMembership(ctx, q, &Membership{
				Scope:            testScope,
				BelongsToUser:    member.ID,
				BelongsToAccount: account.ID,
				Roles:            []string{"account_admin"},
			})
		}))

		revived, err := store.GetMembership(t.Context(), testScope, member.ID, account.ID)
		must.NoError(t, err)
		test.EqOp(t, original.ID, revived.ID)
		test.EqOp(t, original.CreatedAt, revived.CreatedAt)
		test.Eq(t, []string{"account_admin"}, revived.Roles)

		roster, err := store.ListAccountMembers(t.Context(), testScope, account.ID, nil)
		must.NoError(t, err)
		must.SliceLen(t, 2, roster.Data)
	})

	t.Run("replaces roles rather than merging them", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		owner := createUser(t, store, newUser("ada"))
		account := createAccountFor(t, store, owner, "Acme")
		member := registerInto(t, store, newUser("brian"), account.ID, "account_member", "billing_admin")

		before, err := store.GetMembership(t.Context(), testScope, member.ID, account.ID)
		must.NoError(t, err)
		test.Eq(t, []string{"account_member", "billing_admin"}, before.Roles)

		// Revocation is the operation that matters, and a merging setter cannot
		// express it.
		must.NoError(t, store.SetMembershipRoles(t.Context(), testScope, member.ID, account.ID,
			[]string{"account_member"}))

		after, err := store.GetMembership(t.Context(), testScope, member.ID, account.ID)
		must.NoError(t, err)
		test.Eq(t, []string{"account_member"}, after.Roles)

		must.ErrorIs(t,
			store.SetMembershipRoles(t.Context(), testScope, member.ID, account.ID, nil),
			platformerrors.ErrEmptyInputParameter,
		)

		must.ErrorIs(t,
			store.SetMembershipRoles(t.Context(), testScope, member.ID, "not-an-account", []string{"x"}),
			ErrMembershipNotFound,
		)
	})

	t.Run("refuses to remove the last owner", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		owner := createUser(t, store, newUser("ada"))
		account := createAccountFor(t, store, owner, "Acme")

		// An ownerless account fails every permission check that resolves
		// through its owner, far from the removal that caused it.
		must.ErrorIs(t,
			store.RemoveMembership(t.Context(), testScope, owner.ID, account.ID),
			ErrLastAccountOwner,
		)

		still, err := store.GetMembership(t.Context(), testScope, owner.ID, account.ID)
		must.NoError(t, err)
		test.False(t, still.Archived())
	})

	t.Run("moves the default when the default is removed", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		owner := createUser(t, store, newUser("ada"))
		member := createUser(t, store, newUser("brian"))

		first := createAccountFor(t, store, owner, "First")
		second := createAccountFor(t, store, owner, "Second")

		must.NoError(t, inTransaction(t, store, func(ctx context.Context, q database.SQLQueryExecutor) error {
			for _, accountID := range []string{first.ID, second.ID} {
				if err := store.CreateMembership(ctx, q, &Membership{
					Scope:            testScope,
					BelongsToUser:    member.ID,
					BelongsToAccount: accountID,
					Roles:            []string{"account_member"},
				}); err != nil {
					return err
				}
			}

			return nil
		}))

		memberships, err := store.ListMembershipsForUser(t.Context(), testScope, member.ID)
		must.NoError(t, err)
		must.SliceLen(t, 2, memberships)

		defaultAccountID := memberships[0].BelongsToAccount
		test.True(t, memberships[0].DefaultAccount)

		must.NoError(t, store.RemoveMembership(t.Context(), testScope, member.ID, defaultAccountID))

		// A user left with memberships and no default cannot build a Principal,
		// and the failure would surface at their next request rather than here.
		remaining, err := store.ListMembershipsForUser(t.Context(), testScope, member.ID)
		must.NoError(t, err)
		must.SliceLen(t, 1, remaining)
		test.True(t, remaining[0].DefaultAccount)

		must.ErrorIs(t,
			store.RemoveMembership(t.Context(), testScope, member.ID, defaultAccountID),
			ErrMembershipNotFound,
		)
	})

	t.Run("transfers ownership and keeps the old owner on", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		owner := createUser(t, store, newUser("ada"))
		account := createAccountFor(t, store, owner, "Acme")
		successor := createUser(t, store, newUser("grace"))

		// The successor is not yet a member. An owner who is not on the roster
		// is refused by every roster-driven permission check.
		must.NoError(t, store.TransferAccountOwnership(t.Context(), testScope, account.ID, successor.ID))

		read, err := store.GetAccount(t.Context(), testScope, account.ID)
		must.NoError(t, err)
		test.EqOp(t, successor.ID, read.OwnerUserID)

		_, err = store.GetMembership(t.Context(), testScope, successor.ID, account.ID)
		must.NoError(t, err)

		// Transferring and ejecting are different acts; handing over and staying
		// on is the common case.
		_, err = store.GetMembership(t.Context(), testScope, owner.ID, account.ID)
		must.NoError(t, err)

		// The former owner can now be removed, which they could not be before.
		must.NoError(t, store.RemoveMembership(t.Context(), testScope, owner.ID, account.ID))

		// Transferring to the current owner is a no-op rather than an error.
		must.NoError(t, store.TransferAccountOwnership(t.Context(), testScope, account.ID, successor.ID))

		must.ErrorIs(t,
			store.TransferAccountOwnership(t.Context(), testScope, account.ID, ""),
			platformerrors.ErrEmptyInputParameter,
		)
	})

	t.Run("keeps an existing member's roles on transfer", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		owner := createUser(t, store, newUser("ada"))
		account := createAccountFor(t, store, owner, "Acme")
		successor := registerInto(t, store, newUser("grace"), account.ID, "billing_admin")

		must.NoError(t, store.TransferAccountOwnership(t.Context(), testScope, account.ID, successor.ID))

		membership, err := store.GetMembership(t.Context(), testScope, successor.ID, account.ID)
		must.NoError(t, err)
		test.Eq(t, []string{"billing_admin"}, membership.Roles)
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

	t.Run("builds a principal", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		user := newUser("ada")
		user.ServiceRoles = []string{"service_admin"}
		createUser(t, store, user)

		first := createAccountFor(t, store, user, "First", "account_admin")
		second := createAccountFor(t, store, user, "Second", "account_member")

		principal, err := store.GetPrincipal(t.Context(), testScope, user.ID, "")
		must.NoError(t, err)

		// No account named, so the default answers.
		test.EqOp(t, first.ID, principal.ActiveAccountID)
		test.EqOp(t, "", principal.User.HashedPassword)
		test.Eq(t, []string{"account_admin"}, principal.AccountRoles())
		test.Eq(t, []string{"service_admin"}, principal.ServiceRoles())

		// Roles is the union, so a PolicyResolver cannot be handed half the
		// answer.
		test.Eq(t, []string{"service_admin", "account_admin"}, principal.Roles())
		test.Eq(t, []string{first.ID, second.ID}, principal.AccountIDs())

		switched, err := store.GetPrincipal(t.Context(), testScope, user.ID, second.ID)
		must.NoError(t, err)
		test.EqOp(t, second.ID, switched.ActiveAccountID)
		test.Eq(t, []string{"account_member"}, switched.AccountRoles())
	})

	t.Run("refuses a principal for an account the user is not in", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		owner := createUser(t, store, newUser("ada"))
		outsider := createUser(t, store, newUser("mallory"))

		account := createAccountFor(t, store, owner, "Acme")
		createAccountFor(t, store, outsider, "Elsewhere")

		// The check every hand-built session context eventually forgets. Without
		// it everything downstream trusts the ID it was handed.
		_, err := store.GetPrincipal(t.Context(), testScope, outsider.ID, account.ID)
		must.ErrorIs(t, err, ErrMembershipNotFound)
	})

	t.Run("reports a user with no default account", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		user := createUser(t, store, newUser("ada"))

		_, err := store.GetPrincipal(t.Context(), testScope, user.ID, "")
		must.ErrorIs(t, err, ErrNoDefaultAccount)
	})

	t.Run("refuses a membership that carries no roles", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		owner := createUser(t, store, newUser("ada"))
		account := createAccountFor(t, store, owner, "Acme")
		member := createUser(t, store, newUser("brian"))

		// A user who belongs to an account and may do nothing in it reads at
		// runtime as an authorization bug rather than as a missing field.
		err := inTransaction(t, store, func(ctx context.Context, q database.SQLQueryExecutor) error {
			return store.CreateMembership(ctx, q, &Membership{
				Scope:            testScope,
				BelongsToUser:    member.ID,
				BelongsToAccount: account.ID,
			})
		})
		must.Error(t, err)
	})
}
