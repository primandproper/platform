package identity

import (
	"errors"
	"testing"
	"time"
	// Embedded so the time zone cases below do not depend on the test host
	// carrying a zoneinfo database. It affects this test binary only.
	_ "time/tzdata"

	platformerrors "github.com/primandproper/platform-go/v13/errors"
	"github.com/primandproper/platform-go/v13/identifiers"
	"github.com/primandproper/platform-go/v13/tenancy"

	validation "github.com/go-ozzo/ozzo-validation/v4"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestAccountStatus(T *testing.T) {
	T.Parallel()

	T.Run("closed set", func(t *testing.T) {
		t.Parallel()

		for _, status := range []AccountStatus{StatusUnverified, StatusGood, StatusBanned, StatusTerminated} {
			test.True(t, status.Valid(), test.Sprintf("%q", status))
			test.EqOp(t, string(status), status.String())
		}

		// A column holding a near-miss is a value nothing should switch on.
		for _, status := range []AccountStatus{"", "banned ", "GOOD", "active"} {
			test.False(t, status.Valid(), test.Sprintf("%q", status))
		}
	})

	T.Run("only good standing admits sign-in", func(t *testing.T) {
		t.Parallel()

		test.True(t, StatusGood.AdmitsSignIn())

		// The second copy of this rule would be the one that admits a banned
		// user, which is why it is a method rather than a comparison.
		for _, status := range []AccountStatus{StatusUnverified, StatusBanned, StatusTerminated, "nonsense"} {
			test.False(t, status.AdmitsSignIn(), test.Sprintf("%q", status))
		}
	})
}

func TestBillingStatus(t *testing.T) {
	t.Parallel()

	for _, status := range []BillingStatus{BillingUnpaid, BillingTrial, BillingPaid, BillingSuspended} {
		test.True(t, status.Valid(), test.Sprintf("%q", status))
		test.EqOp(t, string(status), status.String())
	}

	for _, status := range []BillingStatus{"", "free", "PAID"} {
		test.False(t, status.Valid(), test.Sprintf("%q", status))
	}
}

func TestInvitationStatus(t *testing.T) {
	t.Parallel()

	for _, status := range []InvitationStatus{InvitationPending, InvitationAccepted, InvitationRejected, InvitationCancelled} {
		test.True(t, status.Valid(), test.Sprintf("%q", status))
	}

	test.False(t, InvitationPending.Terminal())
	test.True(t, InvitationAccepted.Terminal())
	test.True(t, InvitationRejected.Terminal())
	test.True(t, InvitationCancelled.Terminal())
}

func TestAgreement(t *testing.T) {
	t.Parallel()

	test.True(t, TermsOfService.Valid())
	test.True(t, PrivacyPolicy.Valid())
	test.False(t, Agreement("cookies").Valid())
	test.False(t, Agreement("").Valid())
}

func TestUser_Redacted(T *testing.T) {
	T.Parallel()

	T.Run("clears every credential", func(t *testing.T) {
		t.Parallel()

		user := &User{
			ID:                            "u1",
			Username:                      "ada",
			HashedPassword:                "argon2$secret",
			TwoFactorSecret:               "TOTPSECRET",
			EmailAddressVerificationToken: "tok",
			ServiceRoles:                  []string{"service_admin"},
		}

		redacted := user.Redacted()
		test.EqOp(t, "", redacted.HashedPassword)
		test.EqOp(t, "", redacted.TwoFactorSecret)
		test.EqOp(t, "", redacted.EmailAddressVerificationToken)
		test.EqOp(t, "ada", redacted.Username)

		// The original is untouched: a caller redacting for a response body
		// still holds the value it read.
		test.EqOp(t, "argon2$secret", user.HashedPassword)

		// The roles are cloned, so mutating the copy cannot reach through.
		redacted.ServiceRoles[0] = "root"
		test.EqOp(t, "service_admin", user.ServiceRoles[0])
	})

	T.Run("nil redacts to nil", func(t *testing.T) {
		t.Parallel()

		var user *User
		test.Nil(t, user.Redacted())
	})
}

func TestUser_Predicates(T *testing.T) {
	T.Parallel()

	now := time.Now().UTC()

	T.Run("two factor needs a secret that was proven", func(t *testing.T) {
		t.Parallel()

		// A secret that has been issued and never proven is a QR code somebody
		// may have closed, which is why the check is not "is the secret set".
		test.False(t, (&User{TwoFactorSecret: "SECRET"}).TwoFactorEnabled())
		test.False(t, (&User{TwoFactorSecretVerifiedAt: &now}).TwoFactorEnabled())
		test.True(t, (&User{TwoFactorSecret: "SECRET", TwoFactorSecretVerifiedAt: &now}).TwoFactorEnabled())

		var nilUser *User
		test.False(t, nilUser.TwoFactorEnabled())
		test.False(t, nilUser.EmailAddressVerified())
		test.False(t, nilUser.Archived())
		test.False(t, nilUser.HasServiceRole("x"))
	})

	T.Run("service roles", func(t *testing.T) {
		t.Parallel()

		user := &User{ServiceRoles: []string{"service_user", "service_admin"}}
		test.True(t, user.HasServiceRole("service_admin"))
		test.False(t, user.HasServiceRole("root"))
	})
}

func TestUser_Validate(T *testing.T) {
	T.Parallel()

	valid := func() *User {
		return &User{
			Scope:          tenancy.Global(),
			Username:       "ada",
			EmailAddress:   "ada@example.com",
			HashedPassword: "argon2$x",
			AccountStatus:  StatusGood,
		}
	}

	T.Run("accepts a complete user", func(t *testing.T) {
		t.Parallel()

		must.NoError(t, valid().ValidateWithContext(t.Context()))
	})

	T.Run("requires a scope", func(t *testing.T) {
		t.Parallel()

		user := valid()
		user.Scope = tenancy.Scope{}

		// A user that says nothing about which directory it belongs to is one an
		// application registered by accident.
		must.ErrorIs(t, user.ValidateWithContext(t.Context()), tenancy.ErrNoScope)
	})

	T.Run("requires a password hash", func(t *testing.T) {
		t.Parallel()

		user := valid()
		user.HashedPassword = ""

		// Catches the caller who forgot to hash, which is the failure that
		// writes a plaintext password into the column.
		must.Error(t, user.ValidateWithContext(t.Context()))
	})

	T.Run("refuses an unknown status", func(t *testing.T) {
		t.Parallel()

		user := valid()
		user.AccountStatus = "active"

		must.ErrorIs(t, user.ValidateWithContext(t.Context()), platformerrors.ErrUnrecognizedInputValue)
	})

	T.Run("refuses an address that is not bare", func(t *testing.T) {
		t.Parallel()

		user := valid()
		user.EmailAddress = "Ada <ada@example.com>"

		err := user.ValidateWithContext(t.Context())
		must.Error(t, err)

		// The column is a unique key and is compared against what a sign-in form
		// submits, so the display-name form would store a value that never
		// matches.
		var fieldErrs validation.Errors
		must.True(t, errors.As(err, &fieldErrs))
		must.ErrorIs(t, fieldErrs["emailAddress"], ErrInvalidEmailAddress)
	})

	T.Run("refuses a malformed address", func(t *testing.T) {
		t.Parallel()

		for _, address := range []string{"not-an-address", "ada@", "@example.com", "ada @example.com"} {
			user := valid()
			user.EmailAddress = address

			must.Error(t, user.ValidateWithContext(t.Context()), must.Sprintf("%q", address))
		}
	})

	T.Run("nil is an error, not a panic", func(t *testing.T) {
		t.Parallel()

		var user *User
		must.ErrorIs(t, user.ValidateWithContext(t.Context()), ErrNilUser)
	})

	T.Run("defaults to unverified", func(t *testing.T) {
		t.Parallel()

		user := &User{}
		user.EnsureDefaults()

		// A user who has proven nothing is the honest starting point; admitting
		// them by default is the mistake worth making unspellable.
		test.EqOp(t, StatusUnverified, user.AccountStatus)

		set := &User{AccountStatus: StatusGood}
		set.EnsureDefaults()
		test.EqOp(t, StatusGood, set.AccountStatus)

		var nilUser *User
		nilUser.EnsureDefaults()
	})
}

func TestPrincipal(T *testing.T) {
	T.Parallel()

	build := func() *Principal {
		return &Principal{
			User: &User{ID: "u1", ServiceRoles: []string{"service_admin"}},
			Memberships: []*Membership{
				{BelongsToAccount: "a1", DefaultAccount: true, Roles: []string{"account_admin"}},
				{BelongsToAccount: "a2", Roles: []string{"account_member"}},
			},
			ActiveAccountID: "a1",
		}
	}

	T.Run("resolves the active membership", func(t *testing.T) {
		t.Parallel()

		principal := build()
		must.NotNil(t, principal.ActiveMembership())
		test.EqOp(t, "a1", principal.ActiveMembership().BelongsToAccount)

		principal.ActiveAccountID = "a3"
		test.Nil(t, principal.ActiveMembership())
	})

	T.Run("roles are the union", func(t *testing.T) {
		t.Parallel()

		principal := build()
		test.Eq(t, []string{"account_admin"}, principal.AccountRoles())
		test.Eq(t, []string{"service_admin"}, principal.ServiceRoles())

		// Handing a PolicyResolver half the answer is how an operator's support
		// access stops working inside a customer's account.
		test.Eq(t, []string{"service_admin", "account_admin"}, principal.Roles())

		principal.ActiveAccountID = "a2"
		test.Eq(t, []string{"service_admin", "account_member"}, principal.Roles())
	})

	T.Run("lists accounts default first", func(t *testing.T) {
		t.Parallel()

		test.Eq(t, []string{"a1", "a2"}, build().AccountIDs())
	})

	T.Run("nil is empty, not a panic", func(t *testing.T) {
		t.Parallel()

		var principal *Principal
		test.Nil(t, principal.ActiveMembership())
		test.SliceEmpty(t, principal.Roles())
		test.Nil(t, principal.AccountIDs())
		test.Nil(t, principal.ServiceRoles())
		test.Nil(t, principal.AccountRoles())
	})
}

func TestMembership(T *testing.T) {
	T.Parallel()

	T.Run("validates", func(t *testing.T) {
		t.Parallel()

		valid := &Membership{
			Scope:            tenancy.Global(),
			BelongsToUser:    "u1",
			BelongsToAccount: "a1",
			Roles:            []string{"account_member"},
		}
		must.NoError(t, valid.ValidateWithContext(t.Context()))

		// A membership with no roles is a user who belongs to an account and may
		// do nothing in it.
		noRoles := *valid
		noRoles.Roles = nil
		must.Error(t, noRoles.ValidateWithContext(t.Context()))

		emptyRole := *valid
		emptyRole.Roles = []string{""}
		must.ErrorIs(t, emptyRole.ValidateWithContext(t.Context()), platformerrors.ErrEmptyInputParameter)

		noScope := *valid
		noScope.Scope = tenancy.Scope{}
		must.ErrorIs(t, noScope.ValidateWithContext(t.Context()), tenancy.ErrNoScope)

		var nilMembership *Membership
		must.ErrorIs(t, nilMembership.ValidateWithContext(t.Context()), ErrNilMembership)
	})

	T.Run("predicates", func(t *testing.T) {
		t.Parallel()

		membership := &Membership{Roles: []string{"a", "b"}}
		test.True(t, membership.HasRole("a"))
		test.False(t, membership.HasRole("c"))
		test.False(t, membership.Archived())

		var nilMembership *Membership
		test.False(t, nilMembership.HasRole("a"))
		test.False(t, nilMembership.Archived())
	})
}

func TestBillingAddress(t *testing.T) {
	t.Parallel()

	// An application that never collects an address leaves a zero value rather
	// than seven empty strings, which is what makes this check one comparison.
	empty, filled := BillingAddress{}, BillingAddress{City: "London"}

	test.True(t, empty.Zero())
	test.False(t, filled.Zero())
}

func TestAccount_TimeZone(T *testing.T) {
	T.Parallel()

	validate := func(t *testing.T, zone string) error {
		t.Helper()

		account := &Account{
			ID: identifiers.New(), Scope: testScope, Name: "Acme",
			OwnerUserID: identifiers.New(), BillingStatus: BillingUnpaid,
			TimeZone: zone,
		}

		return account.ValidateWithContext(t.Context())
	}

	T.Run("accepts a loadable zone", func(t *testing.T) {
		t.Parallel()
		test.NoError(t, validate(t, "America/Chicago"))
	})

	T.Run("accepts none stated", func(t *testing.T) {
		t.Parallel()

		// An application in one region never sets this, and refusing the write
		// would make a zone a required field for everybody who has no use for
		// one.
		test.NoError(t, validate(t, ""))
	})

	T.Run("refuses a name that does not load", func(t *testing.T) {
		t.Parallel()

		// The typo is the whole point: it looks right, and stored it would
		// render every date on the account wrong without anything saying so.
		err := validate(t, "America/Chicagoo")

		var fieldErrs validation.Errors
		must.True(t, errors.As(err, &fieldErrs))
		must.ErrorIs(t, fieldErrs["timeZone"], ErrInvalidTimeZone)
		test.ErrorIs(t, fieldErrs["timeZone"], platformerrors.ErrUnrecognizedInputValue)
	})

	T.Run("refuses Local", func(t *testing.T) {
		t.Parallel()

		// It loads, so only an explicit refusal catches it — and what it loads
		// is the reader's TZ environment variable, which makes the same stored
		// value mean different things on two replicas of one service.
		err := validate(t, "Local")

		var fieldErrs validation.Errors
		must.True(t, errors.As(err, &fieldErrs))
		must.ErrorIs(t, fieldErrs["timeZone"], ErrInvalidTimeZone)
	})

	T.Run("resolves to UTC when unstated", func(t *testing.T) {
		t.Parallel()

		loc, err := (&Account{}).Location()
		must.NoError(t, err)
		test.EqOp(t, time.UTC, loc)

		// Nil is the case a caller reaches through a not-found read it did not
		// check, and UTC is a better answer there than a panic.
		loc, err = (*Account)(nil).Location()
		must.NoError(t, err)
		test.EqOp(t, time.UTC, loc)
	})

	T.Run("resolves a stated zone", func(t *testing.T) {
		t.Parallel()

		loc, err := (&Account{TimeZone: "America/Chicago"}).Location()
		must.NoError(t, err)
		test.EqOp(t, "America/Chicago", loc.String())
	})

	T.Run("returns UTC alongside the error", func(t *testing.T) {
		t.Parallel()

		// A caller that would rather degrade than fail uses the location
		// regardless, so it must never be nil.
		loc, err := (&Account{TimeZone: "America/Chicagoo"}).Location()
		must.Error(t, err)
		test.ErrorIs(t, err, ErrInvalidTimeZone)
		must.NotNil(t, loc)
		test.EqOp(t, time.UTC, loc)
	})
}
