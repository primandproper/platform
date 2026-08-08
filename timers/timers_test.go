package timers

import (
	"context"
	"database/sql"
	"database/sql/driver"
	stderrors "errors"
	"strings"
	"testing"
	"time"

	"github.com/primandproper/platform-go/v10/clock"
	"github.com/primandproper/platform-go/v10/database"
	"github.com/primandproper/platform-go/v10/database/dialect"
	databasemock "github.com/primandproper/platform-go/v10/database/mock"
	platformerrors "github.com/primandproper/platform-go/v10/errors"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// upperCodec is a custom rendering, for the tests that need one that is
// observably not the default.
type upperCodec struct{}

func (upperCodec) EncodeKey(key string) (string, error) { return strings.ToUpper(key), nil }
func (upperCodec) DecodeKey(encoded string) (string, error) {
	return strings.ToLower(encoded), nil
}

// stubClient is a database.Client that only knows its dialect. Every test in
// this file stops at construction, which is the last point before a statement
// would be issued.
type stubClient struct {
	dialect dialect.Dialect
}

var _ database.Client = (*stubClient)(nil)

func (c *stubClient) Dialect() dialect.Dialect          { return c.dialect }
func (c *stubClient) Reader() database.SQLQueryExecutor { return nil }
func (c *stubClient) Writer() database.SQLQueryExecutor { return nil }
func (c *stubClient) Close() error                      { return nil }
func (c *stubClient) CurrentTime() time.Time            { return time.Time{} }
func (c *stubClient) WithTransaction(context.Context, func(database.SQLQueryExecutor) error) error {
	return nil
}

func postgresClient() *stubClient { return &stubClient{dialect: dialect.Postgres} }

func TestNew(T *testing.T) {
	T.Parallel()

	T.Run("builds a set from a minimal config", func(t *testing.T) {
		t.Parallel()

		set, err := New[string](t.Context(), validConfig(), postgresClient())

		must.NoError(t, err)
		test.EqOp(t, "trials", set.Name())
		test.NotNil(t, set.Clock())
	})

	T.Run("rejects the inputs it cannot work without", func(t *testing.T) {
		t.Parallel()

		_, err := New[string](t.Context(), nil, postgresClient())
		test.True(t, stderrors.Is(err, ErrNilConfig))

		_, err = New[string](t.Context(), validConfig(), nil)
		test.True(t, stderrors.Is(err, ErrNilDatabaseClient))

		_, err = New[string](t.Context(), &Config{}, postgresClient())
		test.True(t, stderrors.Is(err, ErrEmptySetName))
	})

	// The SQL is written against Postgres rather than reduced to a portable
	// subset, so anything else is an error rather than a degraded mode that
	// looks like it worked.
	T.Run("rejects a client that does not speak Postgres", func(t *testing.T) {
		t.Parallel()

		for _, d := range []dialect.Dialect{dialect.MySQL, dialect.SQLite} {
			_, err := New[string](t.Context(), validConfig(), &stubClient{dialect: d})

			test.True(t, stderrors.Is(err, dialect.ErrUnsupported), test.Sprintf("dialect %q", d))
		}
	})

	// The prefix reaches an identifier position in the DDL and in every
	// statement, so it is vetted before a single one is rendered.
	T.Run("rejects a prefix that would not be a legal identifier", func(t *testing.T) {
		t.Parallel()

		cfg := validConfig()
		cfg.TablePrefix = "drop table;--"

		_, err := New[string](t.Context(), cfg, postgresClient())

		test.True(t, stderrors.Is(err, dialect.ErrInvalidIdentifier))
	})

	// The channel is bound as text here, but a listener has to render it into a
	// LISTEN, which takes no parameters. Vetting it here keeps that end from
	// having to.
	T.Run("rejects a notify channel that would not be a legal identifier", func(t *testing.T) {
		t.Parallel()

		cfg := validConfig()
		cfg.NotifyChannel = "not a channel"

		_, err := New[string](t.Context(), cfg, postgresClient())

		test.True(t, stderrors.Is(err, dialect.ErrInvalidIdentifier))
	})

	// Option cannot name K, so the compiler cannot catch this. Catching it at
	// construction is what stops a key being written under a rendering nothing
	// will ever decode.
	T.Run("rejects a codec built for another key type", func(t *testing.T) {
		t.Parallel()

		_, err := New[trialID](t.Context(), validConfig(), postgresClient(), WithKeyCodec[string](upperCodec{}))

		test.True(t, stderrors.Is(err, ErrKeyCodecTypeMismatch))
	})

	T.Run("takes a matching codec", func(t *testing.T) {
		t.Parallel()

		set, err := New[string](t.Context(), validConfig(), postgresClient(), WithKeyCodec[string](upperCodec{}))

		must.NoError(t, err)

		encoded, err := set.codec.EncodeKey("t-1")
		must.NoError(t, err)
		test.EqOp(t, "T-1", encoded)
	})

	T.Run("takes a clock", func(t *testing.T) {
		t.Parallel()

		supplied := clock.NewClock()

		set, err := New[string](t.Context(), validConfig(), postgresClient(), WithClock(supplied))

		must.NoError(t, err)
		test.EqOp(t, supplied, set.Clock())
	})
}

func TestTimers_Wait(T *testing.T) {
	T.Parallel()

	// The poll is the backstop that makes both the next-due read and a wakeup
	// safe to lose, so a loop without one would sleep through every timer
	// scheduled during a listener's reconnect.
	T.Run("rejects a non-positive poll", func(t *testing.T) {
		t.Parallel()

		set, err := New[string](t.Context(), validConfig(), postgresClient())
		must.NoError(t, err)

		test.True(t, stderrors.Is(set.Wait(t.Context(), 0), ErrInvalidPollInterval))
		test.True(t, stderrors.Is(set.Wait(t.Context(), -time.Second), ErrInvalidPollInterval))
	})
}

func TestTimers_EmptyBatchesTouchNothing(T *testing.T) {
	T.Parallel()

	// Every one of these would otherwise reach a nil executor. That they do not
	// is the property: a caller looping over an empty result set should not have
	// to guard each call.
	T.Run("no keys means no statement", func(t *testing.T) {
		t.Parallel()

		set, err := New[string](t.Context(), validConfig(), postgresClient())
		must.NoError(t, err)

		test.NoError(t, set.Schedule(t.Context()))
		test.NoError(t, set.Complete(t.Context()))
		test.NoError(t, set.Release(t.Context(), time.Minute, nil))

		cancelled, err := set.Cancel(t.Context())
		test.NoError(t, err)
		test.EqOp(t, int64(0), cancelled)
	})
}

func TestTruncateError(T *testing.T) {
	T.Parallel()

	T.Run("a nil cause stores nothing", func(t *testing.T) {
		t.Parallel()

		test.Nil(t, truncateError(nil))
	})

	T.Run("bounds what reaches the column", func(t *testing.T) {
		t.Parallel()

		stored, ok := truncateError(stderrors.New(strings.Repeat("e", maxStoredErrLen*2))).(string)

		must.True(t, ok)
		test.EqOp(t, maxStoredErrLen, len(stored))
	})
}

// The lease guard sits above every statement, so a caller who passes a
// meaningless lease is told so rather than leasing rows nobody can hold.
func TestTimers_Claim_RejectsANonPositiveLease(T *testing.T) {
	T.Parallel()

	for _, lease := range []time.Duration{0, -time.Second} {
		set, err := New[string](T.Context(), validConfig(), postgresClient())
		must.NoError(T, err)

		_, claimErr := set.Claim(T.Context(), 1, lease)

		test.True(T, stderrors.Is(claimErr, ErrInvalidLease), test.Sprintf("lease %s", lease))
	}
}

// isRetryable decides which failures withRetries is allowed to re-run. Getting
// it wrong in either direction is expensive: too narrow and a retryable deadlock
// becomes a failed request, too wide and a permanent error is re-run until the
// attempt ceiling.
func TestIsRetryable(T *testing.T) {
	T.Parallel()

	cases := map[string]struct {
		err      error
		expected bool
	}{
		"a deadlock":                 {&pgconn.PgError{Code: pgDeadlockDetected}, true},
		"a serialization failure":    {&pgconn.PgError{Code: pgSerializationFailure}, true},
		"a wrapped deadlock":         {platformerrors.Wrap(&pgconn.PgError{Code: pgDeadlockDetected}, "writing"), true},
		"another Postgres condition": {&pgconn.PgError{Code: "23505"}, false},
		"an unrelated error":         {stderrors.New("boom"), false},
		"no error at all":            {nil, false},
	}

	for name, tc := range cases {
		T.Run(name, func(t *testing.T) {
			t.Parallel()

			test.EqOp(t, tc.expected, isRetryable(tc.err))
		})
	}
}

func TestTimers_WithRetries(T *testing.T) {
	T.Parallel()

	deadlock := &pgconn.PgError{Code: pgDeadlockDetected}

	newSet := func(t *testing.T) *Timers[string] {
		t.Helper()

		set, err := New[string](t.Context(), validConfig(), postgresClient())
		must.NoError(t, err)

		return set
	}

	T.Run("runs the write once when it succeeds", func(t *testing.T) {
		t.Parallel()

		var calls int

		err := newSet(t).withRetries(t.Context(), "writing", func() error {
			calls++

			return nil
		})

		must.NoError(t, err)
		test.EqOp(t, 1, calls)
	})

	// The point of the classification: a permanent failure must not be re-run,
	// because re-asking an answered question only delays the error.
	T.Run("returns a non-retryable failure without re-running it", func(t *testing.T) {
		t.Parallel()

		var calls int
		sentinel := stderrors.New("constraint violated")

		err := newSet(t).withRetries(t.Context(), "writing", func() error {
			calls++

			return sentinel
		})

		test.True(t, stderrors.Is(err, sentinel))
		test.EqOp(t, 1, calls)
	})

	T.Run("re-runs a deadlock until it succeeds", func(t *testing.T) {
		t.Parallel()

		var calls int

		err := newSet(t).withRetries(t.Context(), "writing", func() error {
			calls++
			if calls == 1 {
				return deadlock
			}

			return nil
		})

		must.NoError(t, err)
		test.EqOp(t, 2, calls)
	})

	// WriteAttempts is a ceiling, not a suggestion — a deadlock that never
	// clears has to stop rather than spin.
	T.Run("gives up at the attempt ceiling and returns the last failure", func(t *testing.T) {
		t.Parallel()

		var calls int

		set := newSet(t)

		err := set.withRetries(t.Context(), "writing", func() error {
			calls++

			return deadlock
		})

		test.True(t, stderrors.Is(err, deadlock))
		test.EqOp(t, int(set.cfg.WriteAttempts), calls)
	})
}

// failingConnector is a driver whose every connection attempt fails. It exists
// so that the single-row reads have something to fail with: database/sql will
// not let a *sql.Row carrying an error be constructed directly, and a DB built
// on this connector hands one back from every QueryRowContext.
type failingConnector struct{ err error }

func (c failingConnector) Connect(context.Context) (driver.Conn, error) { return nil, c.err }
func (c failingConnector) Driver() driver.Driver                        { return failingDriver{} }

type failingDriver struct{}

func (failingDriver) Open(string) (driver.Conn, error) {
	return nil, stderrors.New("failingDriver does not open")
}

// failingClient is a client whose every statement fails. It exercises the layer
// between this package and the driver: each method has to surface what the
// database said rather than reporting a success it did not get.
func failingClient(err error) database.Client {
	db := sql.OpenDB(failingConnector{err: err})

	exec := &databasemock.SQLQueryExecutorMock{
		ExecContextFunc: func(context.Context, string, ...any) (sql.Result, error) {
			return nil, err
		},
		QueryContextFunc: func(context.Context, string, ...any) (*sql.Rows, error) {
			return nil, err
		},
		QueryRowContextFunc: func(ctx context.Context, query string, args ...any) *sql.Row {
			return db.QueryRowContext(ctx, query, args...)
		},
	}

	return &databasemock.ClientMock{
		DialectFunc: func() dialect.Dialect { return dialect.Postgres },
		WriterFunc:  func() database.SQLQueryExecutor { return exec },
		ReaderFunc:  func() database.SQLQueryExecutor { return exec },
		WithTransactionFunc: func(_ context.Context, fn func(database.SQLQueryExecutor) error) error {
			return fn(exec)
		},
	}
}

// A write that fails has to come back as a failure. The risk being pinned is
// the opposite: a scheduling call that swallowed its error would report a timer
// as durable when no row exists, which is the one thing this package promises.
func TestTimers_SurfacesDatabaseFailures(T *testing.T) {
	T.Parallel()

	sentinel := stderrors.New("connection refused")

	newSet := func(t *testing.T) *Timers[string] {
		t.Helper()

		set, err := New[string](t.Context(), validConfig(), failingClient(sentinel))
		must.NoError(t, err)

		return set
	}

	due := Due[string]{Key: "a", RunAt: time.Date(2026, time.August, 21, 9, 0, 0, 0, time.UTC)}

	cases := map[string]func(t *testing.T, set *Timers[string]) error{
		"Schedule": func(t *testing.T, set *Timers[string]) error {
			t.Helper()

			return set.ScheduleAt(t.Context(), "a", time.Now(), nil)
		},
		"Claim": func(t *testing.T, set *Timers[string]) error {
			t.Helper()

			_, err := set.Claim(t.Context(), 1, time.Minute)

			return err
		},
		"Complete": func(t *testing.T, set *Timers[string]) error {
			t.Helper()

			return set.Complete(t.Context(), due)
		},
		"Release": func(t *testing.T, set *Timers[string]) error {
			t.Helper()

			return set.Release(t.Context(), time.Minute, stderrors.New("handler failed"), due)
		},
		"Cancel": func(t *testing.T, set *Timers[string]) error {
			t.Helper()

			_, err := set.Cancel(t.Context(), "a")

			return err
		},
		"Reap": func(t *testing.T, set *Timers[string]) error {
			t.Helper()

			_, err := set.Reap(t.Context())

			return err
		},
	}

	for name, call := range cases {
		T.Run(name, func(t *testing.T) {
			t.Parallel()

			err := call(t, newSet(t))

			test.True(t, stderrors.Is(err, sentinel))
		})
	}
}

// Validation runs after defaulting, so what reaches it is a value that was set
// rather than one that was left blank. A sub-millisecond wake floor is the
// shape that survives EnsureDefaults and still has to be refused.
func TestNew_RejectsAConfigThatSurvivesDefaulting(T *testing.T) {
	T.Parallel()

	_, err := New[string](T.Context(),
		&Config{Name: "trials", MinWakeInterval: time.Nanosecond}, postgresClient())

	must.Error(T, err)
	test.StrContains(T, err.Error(), "validating timers config")
}

// Cancel encodes before it writes, so a key the codec rejects is reported
// rather than sent to the database as an empty primary key.
func TestTimers_Cancel_RejectsAKeyItCannotEncode(T *testing.T) {
	T.Parallel()

	set, err := New[string](T.Context(), validConfig(), postgresClient())
	must.NoError(T, err)

	_, err = set.Cancel(T.Context(), "")

	test.True(T, stderrors.Is(err, ErrEmptyKey))
}

// Stats reads a single row, so its failure arrives through Scan rather than
// from the query. It still has to come back as an error rather than as zeroed
// counters, which would read as a healthy, empty set.
func TestTimers_Stats_SurfacesAFailedRead(T *testing.T) {
	T.Parallel()

	sentinel := stderrors.New("connection refused")

	set, err := New[string](T.Context(), validConfig(), failingClient(sentinel))
	must.NoError(T, err)

	_, err = set.Stats(T.Context())

	test.True(T, stderrors.Is(err, sentinel))
}
