package identity

import (
	"testing"

	platformerrors "github.com/primandproper/platform-go/v13/errors"
	"github.com/primandproper/platform-go/v13/pointer"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// runProfileWriterSuite covers what a user or an account may change about
// itself, and the columns those writes must leave alone.
func runProfileWriterSuite(t *testing.T, env *storeEnv) {
	t.Helper()

	t.Run("updates the profile without touching credentials", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		user := createUser(t, store, newUser("ada"))

		must.NoError(t, store.UpdateUserPassword(t.Context(), testScope, user.ID, "argon2$rotated"))

		// A caller writing back the User it read before the rotation. The
		// profile lands; the stale hash does not.
		user.FirstName = "Augusta"
		user.HashedPassword = "argon2$stale"
		must.NoError(t, store.UpdateUser(t.Context(), user))

		read, err := store.GetUser(t.Context(), testScope, user.ID)
		must.NoError(t, err)
		test.EqOp(t, "Augusta", read.FirstName)
		test.EqOp(t, "argon2$rotated", read.HashedPassword)
	})

	t.Run("clears verification when the email address changes", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		user := newUser("ada")
		user.EmailAddressVerificationToken = "verify-me"
		createUser(t, store, user)

		must.NoError(t, store.MarkUserEmailAddressVerified(t.Context(), testScope, user.ID, "verify-me"))

		user.EmailAddress = "moved@example.com"
		must.NoError(t, store.UpdateUser(t.Context(), user))

		read, err := store.GetUser(t.Context(), testScope, user.ID)
		must.NoError(t, err)
		test.False(t, read.EmailAddressVerified())

		// Saving again without changing the address must not re-clear anything
		// it did not have to.
		must.NoError(t, store.UpdateUser(t.Context(), user))
	})

	t.Run("lets a user keep their own handle on update", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		user := createUser(t, store, newUser("ada"))

		user.FirstName = "Augusta"
		must.NoError(t, store.UpdateUser(t.Context(), user))
	})

	t.Run("refuses an update onto somebody else's handle", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		createUser(t, store, newUser("ada"))
		grace := createUser(t, store, newUser("grace"))

		grace.Username = "ada"
		must.ErrorIs(t, store.UpdateUser(t.Context(), grace), ErrUsernameTaken)
	})

	t.Run("records agreements", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		user := createUser(t, store, newUser("ada"))

		must.NoError(t, store.RecordAgreement(t.Context(), testScope, user.ID, TermsOfService, PrivacyPolicy))

		read, err := store.GetUser(t.Context(), testScope, user.ID)
		must.NoError(t, err)
		must.NotNil(t, read.LastAcceptedTermsOfService)
		must.NotNil(t, read.LastAcceptedPrivacyPolicy)

		must.ErrorIs(t, store.RecordAgreement(t.Context(), testScope, user.ID), platformerrors.ErrEmptyInputParameter)
		must.ErrorIs(t,
			store.RecordAgreement(t.Context(), testScope, user.ID, Agreement("cookies")),
			platformerrors.ErrUnrecognizedInputValue,
		)
	})

	t.Run("writes name and address but not billing or owner", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		owner := createUser(t, store, newUser("ada"))
		other := createUser(t, store, newUser("grace"))
		account := createAccountFor(t, store, owner, "Acme")

		must.NoError(t, store.UpdateAccountBilling(t.Context(), testScope, account.ID, &BillingUpdate{
			Status: pointer.To(BillingPaid),
		}))

		// A caller writing back the Account it read before the webhook landed.
		// The name lands; the stale billing status and a substituted owner do
		// not.
		account.Name = "Acme Ltd"
		account.BillingAddress = BillingAddress{Line1: "1 High St", City: "London", Country: "GB"}
		account.BillingStatus = BillingUnpaid
		account.OwnerUserID = other.ID

		must.NoError(t, store.UpdateAccount(t.Context(), account))

		read, err := store.GetAccount(t.Context(), testScope, account.ID)
		must.NoError(t, err)
		test.EqOp(t, "Acme Ltd", read.Name)
		test.EqOp(t, "1 High St", read.BillingAddress.Line1)
		test.EqOp(t, BillingPaid, read.BillingStatus)
		test.EqOp(t, owner.ID, read.OwnerUserID)
	})

	t.Run("round-trips a time zone", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		owner := createUser(t, store, newUser("ada"))
		account := createAccountFor(t, store, owner, "Acme")

		// An account with none stated reads back as none stated rather than as
		// whatever the database or the driver would default to.
		test.EqOp(t, "", account.TimeZone)

		account.TimeZone = "America/Chicago"
		must.NoError(t, store.UpdateAccount(t.Context(), account))

		read, err := store.GetAccount(t.Context(), testScope, account.ID)
		must.NoError(t, err)
		test.EqOp(t, "America/Chicago", read.TimeZone)

		loc, err := read.Location()
		must.NoError(t, err)
		test.EqOp(t, "America/Chicago", loc.String())
	})
}
