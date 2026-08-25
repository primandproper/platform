package identity

import (
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// runSignInReaderSuite covers the two sign-in lookups and the principal read
// every authenticated request afterwards makes.
func runSignInReaderSuite(t *testing.T, env *storeEnv) {
	t.Helper()

	t.Run("reads by username and email, live only", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		user := createUser(t, store, newUser("ada"))

		byName, err := store.GetUserByUsername(t.Context(), testScope, "ada")
		must.NoError(t, err)
		test.EqOp(t, user.ID, byName.ID)

		byEmail, err := store.GetUserByEmailAddress(t.Context(), testScope, "ada@example.com")
		must.NoError(t, err)
		test.EqOp(t, user.ID, byEmail.ID)

		must.NoError(t, store.ArchiveUser(t.Context(), testScope, user.ID))

		// Every read by id excludes archived users too, now that they all run
		// querygen's single-row statement. A caller who wants an archived user
		// back wants a different query rather than a flag on this one.
		_, err = store.GetUserByUsername(t.Context(), testScope, "ada")
		must.ErrorIs(t, err, ErrUserNotFound)

		_, err = store.GetUser(t.Context(), testScope, user.ID)
		must.ErrorIs(t, err, ErrUserNotFound)

		_, err = store.GetPrincipal(t.Context(), testScope, user.ID, "")
		must.ErrorIs(t, err, ErrUserNotFound)
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
}
