package identity

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/primandproper/platform-go/v13/clock"
	clockmock "github.com/primandproper/platform-go/v13/clock/mock"
	"github.com/primandproper/platform-go/v13/database"
	"github.com/primandproper/platform-go/v13/database/dialect"
	"github.com/primandproper/platform-go/v13/database/sqlite"
	"github.com/primandproper/platform-go/v13/identifiers"
	"github.com/primandproper/platform-go/v13/identity/migrations"
	"github.com/primandproper/platform-go/v13/tenancy"

	"github.com/shoenig/test/must"
)

// testClientConfig is the minimum database.ClientConfig a SQLite client needs.
type testClientConfig struct {
	connectionString string
}

var _ database.ClientConfig = (*testClientConfig)(nil)

func (c *testClientConfig) GetReadConnectionString() string   { return c.connectionString }
func (c *testClientConfig) GetWriteConnectionString() string  { return c.connectionString }
func (c *testClientConfig) GetMaxPingAttempts() uint64        { return 1 }
func (c *testClientConfig) GetPingWaitPeriod() time.Duration  { return time.Millisecond }
func (c *testClientConfig) GetMaxIdleConns() int              { return 2 }
func (c *testClientConfig) GetMaxOpenConns() int              { return 1 }
func (c *testClientConfig) GetConnMaxLifetime() time.Duration { return time.Minute }

// The directories the suite registers users in. testScope is what a
// multi-directory consumer passes; otherScope is the neighbor whose rows must
// never appear in testScope's answers.
var (
	testScope  = tenancy.Of("dir_1")
	otherScope = tenancy.Of("dir_2")
)

// prefixCounter names a fresh set of tables per subtest. Subtests share one
// database and must not share tables — a directory read is global to the users
// table within a scope, so one test's users would be another's.
var prefixCounter atomic.Uint64

// storeEnv is one live database plus the dialect to emit SQL for.
type storeEnv struct {
	client  database.Client
	dialect dialect.Dialect
}

// newStore migrates a uniquely prefixed set of identity tables and returns a
// Store over them.
func (e *storeEnv) newStore(t *testing.T) *SQLStore {
	t.Helper()

	store, err := NewSQLStore(e.client, WithTablePrefix(e.migrate(t)))
	must.NoError(t, err)

	return store
}

// migrate renders a uniquely prefixed set of identity tables and returns the
// prefix.
func (e *storeEnv) migrate(t *testing.T) string {
	t.Helper()

	prefix := fmt.Sprintf("id_%d", prefixCounter.Add(1))

	stmts, err := migrations.Statements(e.dialect, prefix)
	must.NoError(t, err)
	must.SliceNotEmpty(t, stmts)

	for _, stmt := range stmts {
		_, execErr := e.client.Writer().ExecContext(t.Context(), stmt)
		must.NoError(t, execErr, must.Sprintf("executing %q", stmt))
	}

	return prefix
}

// newSQLiteEnv builds a SQLite-backed environment. SQLite exercises the real
// SQL — placeholder rendering, the upsert's conflict target, the roster join,
// the LIKE escape — without a container.
func newSQLiteEnv(t *testing.T) *storeEnv {
	t.Helper()

	client, err := sqlite.NewDatabaseClient(t.Context(),
		&testClientConfig{connectionString: filepath.Join(t.TempDir(), "identity.db")})
	must.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	return &storeEnv{client: client, dialect: dialect.SQLite}
}

// fixedClock is a clock the suite can step, for the tests that need a
// registration and an expiry to be a known distance apart.
//
// Built on the generated mock so the methods nothing here calls fail loudly
// instead of lying. This store reads Now and nothing else; a stub returning
// zero from Sleep or a real ticker from NewTicker would quietly absorb a future
// change that started using them.
type fixedClock struct {
	*clockmock.ClockMock

	now time.Time
	mu  sync.Mutex
}

var _ clock.Clock = (*fixedClock)(nil)

func newFixedClock(at time.Time) *fixedClock {
	c := &fixedClock{now: at}

	c.ClockMock = &clockmock.ClockMock{
		NowFunc:   c.read,
		SinceFunc: func(t time.Time) time.Duration { return c.read().Sub(t) },
	}

	return c
}

func (c *fixedClock) read() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.now
}

func (c *fixedClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.now = c.now.Add(d)
}

// baseTime is where the fixed clock starts. A round UTC instant, so a failure
// message reads.
var baseTime = time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)

// newUser builds a valid User in testScope, with everything the store requires
// and nothing it does not.
func newUser(username string) *User {
	return &User{
		ID:             identifiers.New(),
		Scope:          testScope,
		Username:       username,
		EmailAddress:   username + "@example.com",
		FirstName:      "Ada",
		LastName:       "Lovelace",
		HashedPassword: "argon2$" + username,
		AccountStatus:  StatusGood,
	}
}

// newAccount builds a valid Account in testScope owned by ownerID.
func newAccount(name, ownerID string) *Account {
	return &Account{
		ID:            identifiers.New(),
		Scope:         testScope,
		Name:          name,
		OwnerUserID:   ownerID,
		BillingStatus: BillingUnpaid,
	}
}

// createUser writes a user through a transaction, the way a registration would.
func createUser(t *testing.T, store *SQLStore, user *User) *User {
	t.Helper()

	must.NoError(t, store.client.WithTransaction(t.Context(), func(q database.Tx) error {
		return store.CreateUser(t.Context(), q, user)
	}))

	return user
}

// createAccountFor writes an account and the owner's membership, which is what
// a registration does and what almost every test needs before it can start.
func createAccountFor(t *testing.T, store *SQLStore, owner *User, name string, roles ...string) *Account {
	t.Helper()

	if len(roles) == 0 {
		roles = []string{"account_admin"}
	}

	account := newAccount(name, owner.ID)

	must.NoError(t, store.client.WithTransaction(t.Context(), func(q database.Tx) error {
		if err := store.CreateAccount(t.Context(), q, account); err != nil {
			return err
		}

		return store.CreateMembership(t.Context(), q, &Membership{
			Scope:            owner.Scope,
			BelongsToUser:    owner.ID,
			BelongsToAccount: account.ID,
			Roles:            roles,
		})
	}))

	return account
}

// registerInto writes a user and puts them in an existing account.
func registerInto(t *testing.T, store *SQLStore, user *User, accountID string, roles ...string) *User {
	t.Helper()

	if len(roles) == 0 {
		roles = []string{"account_member"}
	}

	must.NoError(t, store.client.WithTransaction(t.Context(), func(q database.Tx) error {
		if err := store.CreateUser(t.Context(), q, user); err != nil {
			return err
		}

		return store.CreateMembership(t.Context(), q, &Membership{
			Scope:            user.Scope,
			BelongsToUser:    user.ID,
			BelongsToAccount: accountID,
			Roles:            roles,
		})
	}))

	return user
}

// newInvitation builds a pending invitation from one user to an address.
func newInvitation(from *User, accountID, toEmail, token string, expires time.Time) *Invitation {
	return &Invitation{
		ID:               identifiers.New(),
		Scope:            from.Scope,
		BelongsToAccount: accountID,
		FromUser:         from.ID,
		ToEmail:          toEmail,
		Token:            token,
		Status:           InvitationPending,
		ExpiresAt:        expires,
		Roles:            []string{"account_member"},
	}
}

// inTransaction runs fn with an executor, for the store methods that take one.
func inTransaction(t *testing.T, store *SQLStore, fn func(ctx context.Context, q database.Tx) error) error {
	t.Helper()

	return store.client.WithTransaction(t.Context(), func(q database.Tx) error {
		return fn(t.Context(), q)
	})
}
