package outbox

import (
	"testing"
	"testing/fstest"

	"github.com/primandproper/platform-go/v7/database"
	"github.com/primandproper/platform-go/v7/database/migrate"
	platformerrors "github.com/primandproper/platform-go/v7/errors"
	loggingnoop "github.com/primandproper/platform-go/v7/observability/logging/noop"
	"github.com/primandproper/platform-go/v7/outbox/migrations"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestNewWriter(T *testing.T) {
	T.Parallel()

	T.Run("accepts every supported dialect", func(t *testing.T) {
		t.Parallel()

		for _, d := range []Dialect{DialectPostgres, DialectMySQL, DialectSQLite} {
			w, err := NewWriter(d)
			must.NoError(t, err)
			test.EqOp(t, DefaultTableName, w.table)
		}
	})

	T.Run("rejects an unknown dialect", func(t *testing.T) {
		t.Parallel()

		_, err := NewWriter("cassandra")
		test.ErrorIs(t, err, ErrUnsupportedDialect)
	})

	T.Run("rejects a table name that is not an identifier", func(t *testing.T) {
		t.Parallel()

		for _, name := range []string{"outbox; DROP TABLE users", "outbox messages", "1outbox", "a.b.c", ""} {
			_, err := NewWriter(DialectSQLite, WithWriterTableName(name))
			if name == "" {
				// An empty override is ignored rather than rejected.
				test.NoError(t, err)

				continue
			}

			test.ErrorIs(t, err, ErrInvalidTableName)
		}
	})

	T.Run("accepts a schema-qualified table name", func(t *testing.T) {
		t.Parallel()

		w, err := NewWriter(DialectPostgres, WithWriterTableName("events.outbox_messages"))
		must.NoError(t, err)
		test.EqOp(t, "events.outbox_messages", w.table)
	})
}

func TestWriter_Enqueue(T *testing.T) {
	T.Parallel()

	T.Run("writes rows inside the caller's transaction", func(t *testing.T) {
		t.Parallel()

		c := newStubClock()
		client := newTestClient(t)

		enqueue(t, client, newTestWriter(t, c),
			Message{Topic: "orders", Payload: map[string]any{"id": "a"}},
			Message{Topic: "shipments", Key: "cart-1", Payload: map[string]any{"id": "b"}},
		)

		test.EqOp(t, 2, countRows(t, client, "1=1"))
		test.EqOp(t, 1, countRows(t, client, "topic = 'shipments' AND partition_key = 'cart-1'"))

		// A new message is immediately eligible.
		test.EqOp(t, 2, countRows(t, client, "next_attempt = created_at AND attempts = 0"))
	})

	T.Run("rolls back with the caller's transaction", func(t *testing.T) {
		t.Parallel()

		c := newStubClock()
		client := newTestClient(t)
		w := newTestWriter(t, c)

		boom := platformerrors.New("caller work failed")

		err := client.WithTransaction(t.Context(), func(q database.SQLQueryExecutor) error {
			if enqueueErr := w.Enqueue(t.Context(), q, Message{Topic: "orders", Payload: map[string]any{"id": "a"}}); enqueueErr != nil {
				return enqueueErr
			}

			// The caller's own work fails after the outbox write.
			return boom
		})

		test.ErrorIs(t, err, boom)

		// This is the entire point of the package: no event survives a rolled
		// back transaction.
		test.EqOp(t, 0, countRows(t, client, "1=1"))
	})

	T.Run("is a no-op with no messages", func(t *testing.T) {
		t.Parallel()

		c := newStubClock()
		client := newTestClient(t)

		enqueue(t, client, newTestWriter(t, c))

		test.EqOp(t, 0, countRows(t, client, "1=1"))
	})

	T.Run("rejects invalid messages", func(t *testing.T) {
		t.Parallel()

		c := newStubClock()
		client := newTestClient(t)
		w := newTestWriter(t, c)

		cases := map[string]struct {
			expected error
			msg      Message
		}{
			"empty topic": {msg: Message{Payload: map[string]any{"id": "a"}}, expected: ErrEmptyTopic},
			"nil payload": {msg: Message{Topic: "orders"}, expected: ErrNilPayload},
		}

		for name, tc := range cases {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				err := client.WithTransaction(t.Context(), func(q database.SQLQueryExecutor) error {
					return w.Enqueue(t.Context(), q, tc.msg)
				})
				test.ErrorIs(t, err, tc.expected)
			})
		}
	})

	T.Run("rejects a nil executor", func(t *testing.T) {
		t.Parallel()

		w := newTestWriter(t, newStubClock())

		err := w.Enqueue(t.Context(), nil, Message{Topic: "orders", Payload: map[string]any{"id": "a"}})
		test.ErrorIs(t, err, ErrNilExecutor)
	})

	T.Run("rejects a payload that cannot be marshaled", func(t *testing.T) {
		t.Parallel()

		c := newStubClock()
		client := newTestClient(t)
		w := newTestWriter(t, c)

		err := client.WithTransaction(t.Context(), func(q database.SQLQueryExecutor) error {
			return w.Enqueue(t.Context(), q, Message{Topic: "orders", Payload: make(chan int)})
		})
		test.Error(t, err)

		test.EqOp(t, 0, countRows(t, client, "1=1"))
	})
}

// migrateOutboxTable creates the outbox table through database/migrate rather
// than by executing the DDL directly, which is how a consumer that already runs
// migrations should be wiring it up.
func migrateOutboxTable(t *testing.T, client database.Client, dialect migrate.Dialect, mDialect migrations.Dialect, table string) {
	t.Helper()

	ddl, err := migrations.SQL(mDialect, table)
	must.NoError(t, err)

	m, err := migrate.New(dialect, fstest.MapFS{},
		migrate.WithGeneratedMigration(1, "create_"+table, ddl),
		migrate.WithLogger(loggingnoop.NewLogger()),
	)
	must.NoError(t, err)

	raw, ok := client.(database.RawAccess)
	must.True(t, ok, must.Sprint("client does not expose RawAccess"))

	must.NoError(t, m.Migrate(t.Context(), raw.WriteDB()))

	// Idempotent, so a second replica booting against the same database is a
	// no-op rather than a failure.
	must.NoError(t, m.Migrate(t.Context(), raw.WriteDB()))
}

func TestOutbox_MigratorIntegration(T *testing.T) {
	T.Parallel()

	T.Run("sqlite table created via migrate is usable", func(t *testing.T) {
		t.Parallel()

		const table = "migrated_outbox"

		c := newStubClock()
		client := newTestClient(t)

		migrateOutboxTable(t, client, migrate.DialectSQLite, migrations.DialectSQLite, table)

		w, err := NewWriter(DialectSQLite, WithWriterClock(c), WithWriterTableName(table))
		must.NoError(t, err)

		relay, rec := newTestRelay(t, client, c, func(cfg *RelayConfig) {
			cfg.TableName = table
		})

		must.NoError(t, client.WithTransaction(t.Context(), func(q database.SQLQueryExecutor) error {
			return w.Enqueue(t.Context(), q, Message{Topic: "orders", Payload: map[string]any{"id": "a"}})
		}))

		relay.cycle(t.Context())

		test.Eq(t, []string{`{"id":"a"}`}, rec.payloads())
	})
}

func TestDialect_Valid(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		test.True(t, DialectPostgres.Valid())
		test.True(t, DialectMySQL.Valid())
		test.True(t, DialectSQLite.Valid())
		test.False(t, Dialect("").Valid())
		test.False(t, Dialect("cassandra").Valid())

		test.True(t, DialectPostgres.supportsSkipLocked())
		test.True(t, DialectMySQL.supportsSkipLocked())
		test.False(t, DialectSQLite.supportsSkipLocked())
	})
}
