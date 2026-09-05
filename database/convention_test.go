package database_test

import (
	"context"
	stderrors "errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/primandproper/platform-go/v14/database"
	"github.com/primandproper/platform-go/v14/database/sqlite"
	platformerrors "github.com/primandproper/platform-go/v14/errors"
	"github.com/primandproper/platform-go/v14/tenancy"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// The tests in this file are the store-signature convention executed rather than
// described: a write takes (ctx, tx database.Tx, scope tenancy.Scope, ...) and a
// read takes (ctx, q database.SQLQueryExecutor, scope tenancy.Scope, ...). The
// asymmetry is the interesting part, and it is what conventionStore below exists
// to pin — the read is given the wider of the two types precisely so that it can
// be called with the Tx a write was just handed, and see that write.
//
// That is a property of this seam and not of any one provider, so it is exercised
// against a real database rather than a mock: sqlite, which needs no container and
// no CGO. sqlmock would answer whatever the test told it to.

// conventionClientConfig is the smallest database.ClientConfig that opens a
// throwaway sqlite file.
type conventionClientConfig struct {
	connectionString string
}

var _ database.ClientConfig = (*conventionClientConfig)(nil)

func (c *conventionClientConfig) GetReadConnectionString() string  { return c.connectionString }
func (c *conventionClientConfig) GetWriteConnectionString() string { return c.connectionString }
func (*conventionClientConfig) GetMaxPingAttempts() uint64         { return 1 }
func (*conventionClientConfig) GetPingWaitPeriod() time.Duration   { return time.Millisecond }
func (*conventionClientConfig) GetMaxIdleConns() int               { return 2 }
func (*conventionClientConfig) GetMaxOpenConns() int               { return 2 }
func (*conventionClientConfig) GetConnMaxLifetime() time.Duration  { return time.Minute }

// conventionStore is a store with one write and one read, written in the shapes
// CLAUDE.md prescribes and nothing else.
type conventionStore struct{}

// createThing is the write shape. It takes the caller's transaction, so it cannot
// commit, cannot roll back, and cannot be reached by a caller holding only
// Client.Writer() — database.Tx is producible only by RunInTransaction.
func (conventionStore) createThing(ctx context.Context, tx database.Tx, scope tenancy.Scope, id string) error {
	_, err := tx.ExecContext(ctx, "INSERT INTO things (id, belongs_to) VALUES (?, ?)", id, scope)

	return err
}

// countThings is the read shape. It takes the wider executor, so one method serves
// a caller holding Client.Reader() and a caller inside a transaction alike.
func (conventionStore) countThings(ctx context.Context, q database.SQLQueryExecutor, scope tenancy.Scope) (int, error) {
	var count int
	err := q.QueryRowContext(ctx, "SELECT COUNT(*) FROM things WHERE belongs_to = ?", scope).Scan(&count)

	return count, err
}

// newConventionClient opens a sqlite client over a temp file with the one table
// conventionStore writes to.
func newConventionClient(t *testing.T) *sqlite.Client {
	t.Helper()

	ctx := t.Context()

	client, err := sqlite.NewDatabaseClient(ctx, &conventionClientConfig{
		connectionString: filepath.Join(t.TempDir(), "convention.db"),
	})
	must.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	_, err = client.Writer().ExecContext(ctx, "CREATE TABLE things (id TEXT NOT NULL PRIMARY KEY, belongs_to TEXT NOT NULL)")
	must.NoError(t, err)

	return client
}

func TestStoreSignatureConvention(T *testing.T) {
	T.Parallel()

	const thingID = "thing_1"

	scope := tenancy.Of("acct_1")
	store := conventionStore{}

	T.Run("a read taking SQLQueryExecutor sees the transaction's own writes", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		client := newConventionClient(t)

		must.NoError(t, client.WithTransaction(ctx, func(tx database.Tx) error {
			if err := store.createThing(ctx, tx, scope, thingID); err != nil {
				return err
			}

			// The point of the wider read parameter: tx is a SQLQueryExecutor, and
			// the row it just wrote is not committed and not visible anywhere else.
			count, err := store.countThings(ctx, tx, scope)
			if err != nil {
				return err
			}

			test.EqOp(t, 1, count)

			return nil
		}))

		// And once the transaction commits, the same read over the read pool agrees.
		count, err := store.countThings(ctx, client.Reader(), scope)
		must.NoError(t, err)
		test.EqOp(t, 1, count)
	})

	T.Run("and that write is nowhere else if the transaction aborts", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		client := newConventionClient(t)

		sentinel := stderrors.New("abort")

		err := client.WithTransaction(ctx, func(tx database.Tx) error {
			if createErr := store.createThing(ctx, tx, scope, thingID); createErr != nil {
				return createErr
			}

			count, countErr := store.countThings(ctx, tx, scope)
			if countErr != nil {
				return countErr
			}

			test.EqOp(t, 1, count)

			return sentinel
		})
		test.ErrorIs(t, err, sentinel)

		// Returning an error is the callback's only way to abort, and the row goes
		// with it — so the Tx was the only executor that ever saw it.
		count, readErr := store.countThings(ctx, client.Reader(), scope)
		must.NoError(t, readErr)
		test.EqOp(t, 0, count)
	})

	T.Run("a scope the write never named is a driver error, not a wider write", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		client := newConventionClient(t)

		// The zero Scope is the absence of a decision rather than Global(), and it
		// fails at the bind rather than writing an empty owner.
		err := client.WithTransaction(ctx, func(tx database.Tx) error {
			return store.createThing(ctx, tx, tenancy.Scope{}, thingID)
		})
		test.ErrorIs(t, err, tenancy.ErrNoScope)
	})
}

// TestWithTransactionIsTheOneWayIn pins the entry point the store ports cite. There
// is deliberately no database.Atomic(ctx, client, fn) beside it — see the rejection
// recorded on Client.WithTransaction — so this method is the whole of the surface a
// caller with no transaction to join has, and it has to answer for a nil callback
// itself.
func TestWithTransactionIsTheOneWayIn(t *testing.T) {
	t.Parallel()

	client := newConventionClient(t)

	test.ErrorIs(t, client.WithTransaction(t.Context(), nil), platformerrors.ErrNilInputParameter)
}
