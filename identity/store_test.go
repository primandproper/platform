package identity

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/primandproper/platform-go/v13/database"
	platformerrors "github.com/primandproper/platform-go/v13/errors"
	"github.com/primandproper/platform-go/v13/filtering"
	"github.com/primandproper/platform-go/v13/identifiers"
	"github.com/primandproper/platform-go/v13/pointer"
	"github.com/primandproper/platform-go/v13/tenancy"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// TestStore_PartitionsItsMethods holds Store's nine constituent interfaces to a
// partition: every method in exactly one of them.
//
// Go permits overlapping embedded interfaces, so a method added to two of these
// compiles and the duplication is invisible — until a caller reaching for the
// narrow interface finds it can do something the name does not say. Comparing
// the summed method counts against Store's is the cheapest thing that notices,
// and it also fails when a method is added to Store's union without landing in
// any of them.
func TestStore_PartitionsItsMethods(t *testing.T) {
	t.Parallel()

	parts := []struct {
		typ  reflect.Type
		name string
	}{
		{reflect.TypeFor[Registrar](), "Registrar"},
		{reflect.TypeFor[CredentialStore](), "CredentialStore"},
		{reflect.TypeFor[SignInReader](), "SignInReader"},
		{reflect.TypeFor[DirectoryReader](), "DirectoryReader"},
		{reflect.TypeFor[ProfileWriter](), "ProfileWriter"},
		{reflect.TypeFor[MembershipWriter](), "MembershipWriter"},
		{reflect.TypeFor[AdminWriter](), "AdminWriter"},
		{reflect.TypeFor[BillingWriter](), "BillingWriter"},
		{reflect.TypeFor[InvitationStore](), "InvitationStore"},
	}

	seen := map[string]string{}
	total := 0

	for _, part := range parts {
		total += part.typ.NumMethod()

		for method := range part.typ.Methods() {
			method := method.Name
			if other, ok := seen[method]; ok {
				t.Errorf("%s is in both %s and %s", method, other, part.name)
			}

			seen[method] = part.name
		}
	}

	storeType := reflect.TypeFor[Store]()
	test.EqOp(t, storeType.NumMethod(), total)

	// Named separately from the count so a method that exists on Store and in
	// none of the nine is reported as the missing name rather than as a number
	// that is one too small.
	for method := range storeType.Methods() {
		if method := method.Name; seen[method] == "" {
			t.Errorf("Store.%s is in none of the nine interfaces", method)
		}
	}
}

// TestSQLStore_SQLite runs the behavioral suite against SQLite, which every
// developer has and every CI run executes. The same suite runs against real
// servers in containers_test.go.
func TestSQLStore_SQLite(T *testing.T) {
	T.Parallel()

	runStoreSuite(T, newSQLiteEnv(T))
}

// runStoreSuite is the whole behavioral contract, run once per dialect.
//
// It is one function rather than a file of top-level tests so that the
// Postgres, MySQL, and SQLite runs cannot drift: a case added for one dialect
// is a case added for all three, which is the only way the ON CONFLICT/ON
// DUPLICATE KEY split and the partial-index difference stay honest.
func runStoreSuite(t *testing.T, env *storeEnv) {
	t.Helper()

	t.Run("users", func(t *testing.T) {
		t.Parallel()
		runUserSuite(t, env)
	})

	t.Run("accounts", func(t *testing.T) {
		t.Parallel()
		runAccountSuite(t, env)
	})

	t.Run("memberships", func(t *testing.T) {
		t.Parallel()
		runMembershipSuite(t, env)
	})

	t.Run("invitations", func(t *testing.T) {
		t.Parallel()
		runInvitationSuite(t, env)
	})
}

func runUserSuite(t *testing.T, env *storeEnv) {
	t.Helper()

	t.Run("creates and reads back", func(t *testing.T) {
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

	t.Run("refuses a nil executor", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		must.ErrorIs(t, store.CreateUser(t.Context(), nil, newUser("ada")), ErrNilExecutor)
	})

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

		// The sign-in reads exclude archived users; the ID read does not, so a
		// reference from another domain still resolves.
		_, err = store.GetUserByUsername(t.Context(), testScope, "ada")
		must.ErrorIs(t, err, ErrUserNotFound)

		archived, err := store.GetUser(t.Context(), testScope, user.ID)
		must.NoError(t, err)
		test.True(t, archived.Archived())
	})

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
		test.True(t, byName.HasServiceRole("service_admin"))

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

		must.NoError(t, inTransaction(t, store, func(ctx context.Context, q database.SQLQueryExecutor) error {
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

	t.Run("returns times in UTC", func(t *testing.T) {
		t.Parallel()

		clk := newFixedClock(baseTime)

		store, err := NewSQLStore(env.client, WithTablePrefix(env.migrate(t)), WithClock(clk))
		must.NoError(t, err)

		user := createUser(t, store, newUser("ada"))

		read, err := store.GetUser(t.Context(), testScope, user.ID)
		must.NoError(t, err)
		test.EqOp(t, time.UTC, read.CreatedAt.Location())
		test.EqOp(t, baseTime, read.CreatedAt)

		clk.advance(time.Hour)
		must.NoError(t, store.SetUserRequiresPasswordChange(t.Context(), testScope, user.ID, true))

		updated, err := store.GetUser(t.Context(), testScope, user.ID)
		must.NoError(t, err)
		must.NotNil(t, updated.LastUpdatedAt)
		test.EqOp(t, baseTime.Add(time.Hour), *updated.LastUpdatedAt)
	})
}
