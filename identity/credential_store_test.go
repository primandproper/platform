package identity

import (
	"testing"

	platformerrors "github.com/primandproper/platform-go/v13/errors"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// runCredentialStoreSuite covers the single-fact credential writes and the
// verification-token read, none of which a whole-User write can reach.
func runCredentialStoreSuite(t *testing.T, env *storeEnv) {
	t.Helper()

	t.Run("verifies an email address exactly once", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		user := newUser("ada")
		user.EmailAddressVerificationToken = "verify-me"
		createUser(t, store, user)

		found, err := store.GetUserByEmailVerificationToken(t.Context(), testScope, "verify-me")
		must.NoError(t, err)
		test.EqOp(t, user.ID, found.ID)

		must.NoError(t, store.MarkUserEmailAddressVerified(t.Context(), testScope, user.ID, "verify-me"))

		verified, err := store.GetUser(t.Context(), testScope, user.ID)
		must.NoError(t, err)
		test.True(t, verified.EmailAddressVerified())
		test.EqOp(t, "", verified.EmailAddressVerificationToken)

		// The token is burned, so the link cannot be replayed.
		err = store.MarkUserEmailAddressVerified(t.Context(), testScope, user.ID, "verify-me")
		must.ErrorIs(t, err, ErrUserNotFound)

		_, err = store.GetUserByEmailVerificationToken(t.Context(), testScope, "verify-me")
		must.ErrorIs(t, err, ErrUserNotFound)
	})

	t.Run("refuses an empty verification token", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		createUser(t, store, newUser("ada"))

		// Every unverified user's column holds the empty string, so this query
		// would otherwise match an arbitrary one of them.
		_, err := store.GetUserByEmailVerificationToken(t.Context(), testScope, "")
		must.ErrorIs(t, err, platformerrors.ErrEmptyInputParameter)
	})

	t.Run("releases a forced password change on rotation", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		user := createUser(t, store, newUser("ada"))

		must.NoError(t, store.SetUserRequiresPasswordChange(t.Context(), testScope, user.ID, true))

		forced, err := store.GetUser(t.Context(), testScope, user.ID)
		must.NoError(t, err)
		test.True(t, forced.RequiresPasswordChange)

		must.NoError(t, store.UpdateUserPassword(t.Context(), testScope, user.ID, "argon2$new"))

		// Clearing the flag on rotation is what makes a forced change
		// terminate; leaving it set prompts forever.
		rotated, err := store.GetUser(t.Context(), testScope, user.ID)
		must.NoError(t, err)
		test.False(t, rotated.RequiresPasswordChange)
		must.NotNil(t, rotated.PasswordLastChangedAt)
	})

	t.Run("enrolls a second factor unverified", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		user := createUser(t, store, newUser("ada"))

		must.NoError(t, store.UpdateUserTwoFactorSecret(t.Context(), testScope, user.ID, "SECRET"))

		enrolled, err := store.GetUser(t.Context(), testScope, user.ID)
		must.NoError(t, err)
		test.EqOp(t, "SECRET", enrolled.TwoFactorSecret)
		test.False(t, enrolled.TwoFactorEnabled())

		must.NoError(t, store.MarkUserTwoFactorSecretVerified(t.Context(), testScope, user.ID))

		verified, err := store.GetUser(t.Context(), testScope, user.ID)
		must.NoError(t, err)
		test.True(t, verified.TwoFactorEnabled())

		// Verifying twice is either a replay or a flow that lost track of
		// itself, and either is worth surfacing.
		must.ErrorIs(t, store.MarkUserTwoFactorSecretVerified(t.Context(), testScope, user.ID), ErrUserNotFound)

		// Re-enrolling drops the proof with the secret.
		must.NoError(t, store.UpdateUserTwoFactorSecret(t.Context(), testScope, user.ID, "ROTATED"))

		rotated, err := store.GetUser(t.Context(), testScope, user.ID)
		must.NoError(t, err)
		test.False(t, rotated.TwoFactorEnabled())
	})
}
