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

// runClockSuite covers the timestamps, which come from two clocks and no longer
// from one.
//
// created_at is the database's — the create does not carry the column and the
// schema defaults it — so a fixed store clock cannot pin it, and what is worth
// pinning instead is that the value reaches the caller's struct at all. The
// writes querygen does not render still stamp last_updated_at from the store's
// clock, and those a fixed clock does pin.
func runClockSuite(t *testing.T, env *storeEnv) {
	t.Helper()

	t.Run("returns times in UTC", func(t *testing.T) {
		t.Parallel()

		clk := newFixedClock(baseTime)

		store, err := NewSQLStore(env.client, WithTablePrefix(env.migrate(t)), WithClock(clk))
		must.NoError(t, err)

		user := createUser(t, store, newUser("ada"))

		// The database assigned it and the create read it back, so the caller's
		// copy is not the zero time — which is what a service serializes into a
		// response straight after creating a user.
		test.False(t, user.CreatedAt.IsZero())

		read, err := store.GetUser(t.Context(), testScope, user.ID)
		must.NoError(t, err)
		test.EqOp(t, time.UTC, read.CreatedAt.Location())
		test.EqOp(t, user.CreatedAt, read.CreatedAt)

		clk.advance(time.Hour)
		must.NoError(t, store.SetUserRequiresPasswordChange(t.Context(), testScope, user.ID, true))

		updated, err := store.GetUser(t.Context(), testScope, user.ID)
		must.NoError(t, err)
		must.NotNil(t, updated.LastUpdatedAt)
		test.EqOp(t, baseTime.Add(time.Hour), *updated.LastUpdatedAt)
	})

	t.Run("stamps account times in UTC", func(t *testing.T) {
		t.Parallel()

		clk := newFixedClock(baseTime)

		store, err := NewSQLStore(env.client, WithTablePrefix(env.migrate(t)), WithClock(clk))
		must.NoError(t, err)

		owner := createUser(t, store, newUser("ada"))
		account := createAccountFor(t, store, owner, "Acme")

		test.False(t, account.CreatedAt.IsZero())

		read, err := store.GetAccount(t.Context(), testScope, account.ID)
		must.NoError(t, err)
		test.EqOp(t, time.UTC, read.CreatedAt.Location())
		test.EqOp(t, account.CreatedAt, read.CreatedAt)
	})
}
