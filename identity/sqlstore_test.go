package identity

import (
	"testing"
	"time"

	"github.com/primandproper/platform-go/v13/database"
	"github.com/primandproper/platform-go/v13/database/dialect"
	databasemock "github.com/primandproper/platform-go/v13/database/mock"
	platformerrors "github.com/primandproper/platform-go/v13/errors"
	"github.com/primandproper/platform-go/v13/filtering"
	"github.com/primandproper/platform-go/v13/observability"
	"github.com/primandproper/platform-go/v13/tenancy"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// mockClient answers Dialect and nothing else, which is all construction
// reaches for.
func mockClient(d dialect.Dialect) database.Client {
	return &databasemock.ClientMock{DialectFunc: func() dialect.Dialect { return d }}
}

func TestNewSQLStore(T *testing.T) {
	T.Parallel()

	T.Run("builds against every dialect", func(t *testing.T) {
		t.Parallel()

		for _, d := range allDialects {
			store, err := NewSQLStore(mockClient(d))
			must.NoError(t, err, must.Sprintf("%s", d))
			must.NotNil(t, store)

			// The dialect comes off the client, so the two cannot disagree.
			test.EqOp(t, d, store.dialect)
		}
	})

	T.Run("refuses a nil client", func(t *testing.T) {
		t.Parallel()

		store, err := NewSQLStore(nil)
		must.ErrorIs(t, err, ErrNilDatabaseClient)
		must.ErrorIs(t, err, platformerrors.ErrNilInputParameter)
		test.Nil(t, store)
	})

	T.Run("refuses a dialect it cannot emit", func(t *testing.T) {
		t.Parallel()

		store, err := NewSQLStore(mockClient(dialect.Dialect("oracle")))
		must.Error(t, err)
		test.Nil(t, store)
	})

	T.Run("refuses a prefix that cannot render", func(t *testing.T) {
		t.Parallel()

		store, err := NewSQLStore(mockClient(dialect.Postgres), WithTablePrefix("has space"))
		must.Error(t, err)
		test.Nil(t, store)
	})

	T.Run("takes no observability at all", func(t *testing.T) {
		t.Parallel()

		// Absent means noop, so an unconfigured store logs nowhere and traces
		// nowhere rather than requiring three explicit noops.
		store, err := NewSQLStore(mockClient(dialect.SQLite),
			WithStoreLogger(nil),
			WithStoreTracerProvider(nil),
			WithStoreMetricsProvider(nil),
			WithStorePillars(nil),
			nil,
		)
		must.NoError(t, err)
		must.NotNil(t, store)
		must.NotNil(t, store.o11y)
	})

	T.Run("takes pillars", func(t *testing.T) {
		t.Parallel()

		store, err := NewSQLStore(mockClient(dialect.Postgres), WithStorePillars(&observability.Pillars{}))
		must.NoError(t, err)
		must.NotNil(t, store)
	})

	T.Run("ignores a nil clock", func(t *testing.T) {
		t.Parallel()

		store, err := NewSQLStore(mockClient(dialect.Postgres), WithClock(nil))
		must.NoError(t, err)
		must.NotNil(t, store.clock)

		clk := newFixedClock(baseTime)

		stubbed, err := NewSQLStore(mockClient(dialect.Postgres), WithClock(clk))
		must.NoError(t, err)
		test.EqOp(t, baseTime, stubbed.now())
		test.EqOp(t, time.UTC, stubbed.now().Location())
	})

	T.Run("satisfies the interface", func(t *testing.T) {
		t.Parallel()

		store, err := NewSQLStore(mockClient(dialect.Postgres))
		must.NoError(t, err)

		var iface Store = store
		test.NotNil(t, iface)
	})
}

func TestRequireExecutor(t *testing.T) {
	t.Parallel()

	// A nil executor reaching a method call is a nil dereference three frames
	// down rather than the name of the parameter that was not supplied.
	must.ErrorIs(t, requireExecutor(nil), ErrNilExecutor)
	must.NoError(t, requireExecutor(&databasemock.SQLQueryExecutorMock{}))
}

func TestNewID(t *testing.T) {
	t.Parallel()

	// A caller-supplied ID matters for registration, where the user ID is often
	// minted before the transaction so an outbox message can reference it.
	test.EqOp(t, "given", newID("given"))
	test.NotEq(t, "", newID(""))
	test.NotEq(t, newID(""), newID(""))
}

func TestPageWindow(T *testing.T) {
	T.Parallel()

	T.Run("defaults a nil filter", func(t *testing.T) {
		t.Parallel()

		filter, cursor, limit := pageWindow(nil)
		must.NotNil(t, filter)
		test.EqOp(t, "", cursor)
		test.Greater(t, 0, limit)
	})

	T.Run("clamps an over-large page", func(t *testing.T) {
		t.Parallel()

		// A caller that asked for fifty thousand rows gets the ceiling every
		// other paged read in this module applies.
		huge := uint16(65535)
		_, _, limit := pageWindow(&filtering.QueryFilter{MaxResponseSize: &huge})
		test.Less(t, int(huge), limit)
	})
}

func TestResolveActiveAccount(T *testing.T) {
	T.Parallel()

	memberships := []*Membership{
		{BelongsToAccount: "a1", DefaultAccount: true},
		{BelongsToAccount: "a2"},
	}

	T.Run("answers an empty request with the default", func(t *testing.T) {
		t.Parallel()

		active, err := resolveActiveAccount(memberships, "")
		must.NoError(t, err)
		test.EqOp(t, "a1", active)
	})

	T.Run("accepts an account the user is in", func(t *testing.T) {
		t.Parallel()

		active, err := resolveActiveAccount(memberships, "a2")
		must.NoError(t, err)
		test.EqOp(t, "a2", active)
	})

	T.Run("refuses an account the user is not in", func(t *testing.T) {
		t.Parallel()

		// The check every hand-built session context eventually forgets.
		_, err := resolveActiveAccount(memberships, "a3")
		must.ErrorIs(t, err, ErrMembershipNotFound)
	})

	T.Run("reports a user with no default", func(t *testing.T) {
		t.Parallel()

		_, err := resolveActiveAccount(nil, "")
		must.ErrorIs(t, err, ErrNoDefaultAccount)

		_, err = resolveActiveAccount([]*Membership{{BelongsToAccount: "a1"}}, "")
		must.ErrorIs(t, err, ErrNoDefaultAccount)
	})
}

func TestInvitation(T *testing.T) {
	T.Parallel()

	valid := func() *Invitation {
		return &Invitation{
			Scope:            tenancy.Global(),
			BelongsToAccount: "a1",
			FromUser:         "u1",
			ToEmail:          "brian@example.com",
			Token:            "tok",
			Status:           InvitationPending,
			ExpiresAt:        baseTime.Add(time.Hour),
			Roles:            []string{"account_member"},
		}
	}

	T.Run("validates", func(t *testing.T) {
		t.Parallel()

		must.NoError(t, valid().ValidateWithContext(t.Context()))

		// An invitation link is a bearer credential for joining somebody else's
		// account; one that never expires is still valid in a mailbox somebody
		// lost control of two years ago.
		noExpiry := valid()
		noExpiry.ExpiresAt = time.Time{}
		must.ErrorIs(t, noExpiry.ValidateWithContext(t.Context()), platformerrors.ErrEmptyInputParameter)

		noToken := valid()
		noToken.Token = ""
		must.Error(t, noToken.ValidateWithContext(t.Context()))

		noRoles := valid()
		noRoles.Roles = nil
		must.Error(t, noRoles.ValidateWithContext(t.Context()))

		badStatus := valid()
		badStatus.Status = "unknown"
		must.ErrorIs(t, badStatus.ValidateWithContext(t.Context()), platformerrors.ErrUnrecognizedInputValue)

		var nilInvitation *Invitation
		must.ErrorIs(t, nilInvitation.ValidateWithContext(t.Context()), ErrNilInvitation)
	})

	T.Run("expiry is inclusive of the instant it names", func(t *testing.T) {
		t.Parallel()

		invitation := valid()
		test.False(t, invitation.Expired(baseTime))
		test.True(t, invitation.Expired(invitation.ExpiresAt))
		test.True(t, invitation.Expired(invitation.ExpiresAt.Add(time.Second)))

		var nilInvitation *Invitation
		test.False(t, nilInvitation.Expired(baseTime))
	})

	T.Run("redacts the token", func(t *testing.T) {
		t.Parallel()

		redacted := valid().Redacted()
		test.EqOp(t, "", redacted.Token)
		test.EqOp(t, "brian@example.com", redacted.ToEmail)

		var nilInvitation *Invitation
		test.Nil(t, nilInvitation.Redacted())
	})

	T.Run("defaults to pending", func(t *testing.T) {
		t.Parallel()

		invitation := &Invitation{}
		invitation.EnsureDefaults()
		test.EqOp(t, InvitationPending, invitation.Status)

		var nilInvitation *Invitation
		nilInvitation.EnsureDefaults()
	})
}

func TestAccount_Validate(T *testing.T) {
	T.Parallel()

	valid := func() *Account {
		return &Account{
			Scope:         tenancy.Global(),
			Name:          "Acme",
			OwnerUserID:   "u1",
			BillingStatus: BillingUnpaid,
		}
	}

	T.Run("accepts a complete account", func(t *testing.T) {
		t.Parallel()

		must.NoError(t, valid().ValidateWithContext(t.Context()))
	})

	T.Run("requires an owner", func(t *testing.T) {
		t.Parallel()

		// An account with no owner is one whose every ownership-derived
		// permission check resolves to nobody.
		ownerless := valid()
		ownerless.OwnerUserID = ""
		must.Error(t, ownerless.ValidateWithContext(t.Context()))
	})

	T.Run("requires a scope and a known status", func(t *testing.T) {
		t.Parallel()

		noScope := valid()
		noScope.Scope = tenancy.Scope{}
		must.ErrorIs(t, noScope.ValidateWithContext(t.Context()), tenancy.ErrNoScope)

		badStatus := valid()
		badStatus.BillingStatus = "free"
		must.ErrorIs(t, badStatus.ValidateWithContext(t.Context()), platformerrors.ErrUnrecognizedInputValue)

		var nilAccount *Account
		must.ErrorIs(t, nilAccount.ValidateWithContext(t.Context()), ErrNilAccount)
	})

	T.Run("defaults to unpaid", func(t *testing.T) {
		t.Parallel()

		account := &Account{}
		account.EnsureDefaults()
		test.EqOp(t, BillingUnpaid, account.BillingStatus)

		var nilAccount *Account
		nilAccount.EnsureDefaults()
		test.False(t, nilAccount.Archived())
	})
}

func TestBillingUpdate_Empty(t *testing.T) {
	t.Parallel()

	// An update that writes nothing would be an UPDATE with no SET clause.
	test.True(t, (*BillingUpdate)(nil).Empty())
	test.True(t, (&BillingUpdate{}).Empty())

	status := BillingPaid
	test.False(t, (&BillingUpdate{Status: &status}).Empty())
}
