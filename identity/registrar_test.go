package identity

import (
	"context"
	"testing"

	"github.com/primandproper/platform-go/v13/database"
	"github.com/primandproper/platform-go/v13/tenancy"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// runRegistrarSuite covers the three writes that make a registration, each
// through the caller's executor.
func runRegistrarSuite(t *testing.T, env *storeEnv) {
	t.Helper()

	t.Run("creates a user and reads it back", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		user := createUser(t, store, newUser("ada"))

		test.NotEq(t, "", user.ID)
		test.False(t, user.CreatedAt.IsZero())

		read, err := store.GetUser(t.Context(), testScope, user.ID)
		must.NoError(t, err)
		test.EqOp(t, "ada", read.Username)
		test.EqOp(t, "ada@example.com", read.EmailAddress)

		// GetUser is the credential read, so the hash comes back — this is what
		// a sign-in flow compares against.
		test.EqOp(t, user.HashedPassword, read.HashedPassword)
	})

	t.Run("generates an ID when none is given", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		user := newUser("grace")
		user.ID = ""

		createUser(t, store, user)
		test.NotEq(t, "", user.ID)
	})

	t.Run("refuses a taken username", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		createUser(t, store, newUser("ada"))

		second := newUser("ada")
		second.EmailAddress = "different@example.com"

		err := inTransaction(t, store, func(ctx context.Context, q database.SQLQueryExecutor) error {
			return store.CreateUser(ctx, q, second)
		})
		must.ErrorIs(t, err, ErrUsernameTaken)
	})

	t.Run("refuses a taken email address", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		createUser(t, store, newUser("ada"))

		second := newUser("grace")
		second.EmailAddress = "ada@example.com"

		err := inTransaction(t, store, func(ctx context.Context, q database.SQLQueryExecutor) error {
			return store.CreateUser(ctx, q, second)
		})
		must.ErrorIs(t, err, ErrEmailAddressTaken)
	})

	t.Run("allows the same username in another directory", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		createUser(t, store, newUser("ada"))

		// The whole point of the scope being in the unique index: two
		// directories are two namespaces, and one customer's handles do not
		// exhaust another's.
		neighbor := newUser("ada")
		neighbor.Scope = otherScope

		createUser(t, store, neighbor)

		read, err := store.GetUser(t.Context(), otherScope, neighbor.ID)
		must.NoError(t, err)
		test.EqOp(t, "ada", read.Username)
	})

	t.Run("refuses a nil executor", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		must.ErrorIs(t, store.CreateUser(t.Context(), nil, newUser("ada")), ErrNilExecutor)
	})

	t.Run("refuses a user that fails validation", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		noHash := newUser("ada")
		noHash.HashedPassword = ""

		err := inTransaction(t, store, func(ctx context.Context, q database.SQLQueryExecutor) error {
			return store.CreateUser(ctx, q, noHash)
		})
		must.Error(t, err)

		noScope := newUser("grace")
		noScope.Scope = tenancy.Scope{}

		err = inTransaction(t, store, func(ctx context.Context, q database.SQLQueryExecutor) error {
			return store.CreateUser(ctx, q, noScope)
		})
		must.ErrorIs(t, err, tenancy.ErrNoScope)

		badEmail := newUser("grace")
		badEmail.EmailAddress = "Grace <grace@example.com>"

		err = inTransaction(t, store, func(ctx context.Context, q database.SQLQueryExecutor) error {
			return store.CreateUser(ctx, q, badEmail)
		})
		// ozzo collects field errors into a map that does not unwrap, so the
		// sentinel is asserted against the rendered message here and against the
		// error chain in the value-level test.
		must.ErrorContains(t, err, "is not a bare address")
	})

	t.Run("creates an account and reads it back", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		owner := createUser(t, store, newUser("ada"))
		account := createAccountFor(t, store, owner, "Acme")

		read, err := store.GetAccount(t.Context(), testScope, account.ID)
		must.NoError(t, err)
		test.EqOp(t, "Acme", read.Name)
		test.EqOp(t, owner.ID, read.OwnerUserID)
		test.EqOp(t, BillingUnpaid, read.BillingStatus)
		test.True(t, read.BillingAddress.Zero())
	})

	t.Run("refuses an ownerless account", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		orphan := newAccount("Acme", "")

		err := inTransaction(t, store, func(ctx context.Context, q database.SQLQueryExecutor) error {
			return store.CreateAccount(ctx, q, orphan)
		})
		must.Error(t, err)
	})

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
