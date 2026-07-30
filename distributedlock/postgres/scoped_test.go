package postgres

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	cbmock "github.com/primandproper/platform-go/v8/circuitbreaking/mock"
	cbnoop "github.com/primandproper/platform-go/v8/circuitbreaking/noop"
	"github.com/primandproper/platform-go/v8/distributedlock"
	"github.com/primandproper/platform-go/v8/identifiers"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

var (
	xactLockPattern    = regexp.QuoteMeta(`SELECT pg_advisory_xact_lock($1)`)
	tryXactLockPattern = regexp.QuoteMeta(`SELECT pg_try_advisory_xact_lock($1)`)
)

func newTestScopedLocker(t *testing.T, client *testDBClient) distributedlock.ScopedLocker {
	t.Helper()

	s, err := NewPostgresScopedLocker(&Config{}, client, cbnoop.NewCircuitBreaker())
	must.NoError(t, err)
	must.NotNil(t, s)

	return s
}

func TestNewPostgresScopedLocker(T *testing.T) {
	T.Parallel()

	T.Run("rejects nil config", func(t *testing.T) {
		t.Parallel()

		client, _ := buildSqlmockClient(t)

		_, err := NewPostgresScopedLocker(nil, client, cbnoop.NewCircuitBreaker())
		test.ErrorIs(t, err, distributedlock.ErrNilConfig)
	})

	T.Run("rejects nil database client", func(t *testing.T) {
		t.Parallel()

		_, err := NewPostgresScopedLocker(&Config{}, nil, cbnoop.NewCircuitBreaker())
		test.ErrorIs(t, err, distributedlock.ErrNilDatabaseClient)
	})

	// The three counters are built in order, so failing the Nth walks the
	// constructor's error paths one at a time.
	for idx := 1; idx <= 3; idx++ {
		T.Run("int64 counter creation failure", func(t *testing.T) {
			t.Parallel()

			client, _ := buildSqlmockClient(t)

			_, err := NewPostgresScopedLocker(
				&Config{},
				client,
				cbnoop.NewCircuitBreaker(),
				WithMetricsProvider(newErrorAtCallProvider(idx, false)),
			)
			test.Error(t, err)
		})
	}

	T.Run("float64 histogram creation failure", func(t *testing.T) {
		t.Parallel()

		client, _ := buildSqlmockClient(t)

		_, err := NewPostgresScopedLocker(
			&Config{},
			client,
			cbnoop.NewCircuitBreaker(),
			WithMetricsProvider(newErrorAtCallProvider(0, true)),
		)
		test.Error(t, err)
	})
}

func TestPostgresScopedLocker_WithLock_Unit(T *testing.T) {
	T.Parallel()

	T.Run("acquires inside a transaction, runs fn, commits", func(t *testing.T) {
		t.Parallel()

		client, mock := buildSqlmockClient(t)
		s := newTestScopedLocker(t, client)

		mock.ExpectBegin()
		mock.ExpectExec(xactLockPattern).WithArgs(sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectCommit()

		ran := false
		err := s.WithLock(t.Context(), "chore", func(context.Context) error {
			ran = true
			return nil
		})

		must.NoError(t, err)
		test.True(t, ran)
		test.NoError(t, mock.ExpectationsWereMet())
	})

	T.Run("fn error rolls back and passes through", func(t *testing.T) {
		t.Parallel()

		client, mock := buildSqlmockClient(t)
		s := newTestScopedLocker(t, client)

		mock.ExpectBegin()
		mock.ExpectExec(xactLockPattern).WithArgs(sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectRollback()

		boom := errors.New("boom")
		err := s.WithLock(t.Context(), "chore", func(context.Context) error {
			return boom
		})

		test.ErrorIs(t, err, boom)
		test.NoError(t, mock.ExpectationsWereMet())
	})

	T.Run("rejects an empty key", func(t *testing.T) {
		t.Parallel()

		client, _ := buildSqlmockClient(t)
		s := newTestScopedLocker(t, client)

		err := s.WithLock(t.Context(), "", func(context.Context) error { return nil })
		test.ErrorIs(t, err, distributedlock.ErrEmptyKey)
	})

	T.Run("open circuit breaker refuses", func(t *testing.T) {
		t.Parallel()

		client, _ := buildSqlmockClient(t)
		cb := &cbmock.CircuitBreakerMock{
			CannotProceedFunc: func() bool { return true },
		}

		s, err := NewPostgresScopedLocker(&Config{}, client, cb)
		must.NoError(t, err)

		err = s.WithLock(t.Context(), "chore", func(context.Context) error {
			t.Fatal("fn must not run with an open breaker")
			return nil
		})
		test.Error(t, err)
	})

	T.Run("a failed acquire trips the breaker", func(t *testing.T) {
		t.Parallel()

		client, mock := buildSqlmockClient(t)
		var failed int
		cb := &cbmock.CircuitBreakerMock{
			CannotProceedFunc: func() bool { return false },
			FailedFunc:        func() { failed++ },
		}

		s, err := NewPostgresScopedLocker(&Config{}, client, cb)
		must.NoError(t, err)

		mock.ExpectBegin()
		mock.ExpectExec(xactLockPattern).WithArgs(sqlmock.AnyArg()).WillReturnError(errors.New("connection reset"))
		mock.ExpectRollback()

		err = s.WithLock(t.Context(), "chore", func(context.Context) error {
			t.Fatal("fn must not run when the lock was never acquired")
			return nil
		})

		test.Error(t, err)
		// Failing to acquire is an infrastructure signal, unlike an error from
		// fn, which passes through untouched.
		test.EqOp(t, 1, failed)
		test.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestPostgresScopedLocker_TryWithLock_Unit(T *testing.T) {
	T.Parallel()

	T.Run("granted lock runs fn and reports acquired", func(t *testing.T) {
		t.Parallel()

		client, mock := buildSqlmockClient(t)
		s := newTestScopedLocker(t, client)

		mock.ExpectBegin()
		mock.ExpectQuery(tryXactLockPattern).WithArgs(sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"pg_try_advisory_xact_lock"}).AddRow(true))
		mock.ExpectCommit()

		ran := false
		acquired, err := s.TryWithLock(t.Context(), "chore", func(context.Context) error {
			ran = true
			return nil
		})

		must.NoError(t, err)
		test.True(t, acquired)
		test.True(t, ran)
		test.NoError(t, mock.ExpectationsWereMet())
	})

	T.Run("contended lock reports false without running fn", func(t *testing.T) {
		t.Parallel()

		client, mock := buildSqlmockClient(t)
		s := newTestScopedLocker(t, client)

		mock.ExpectBegin()
		mock.ExpectQuery(tryXactLockPattern).WithArgs(sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"pg_try_advisory_xact_lock"}).AddRow(false))
		mock.ExpectRollback()

		acquired, err := s.TryWithLock(t.Context(), "chore", func(context.Context) error {
			t.Fatal("fn must not run when the lock is contended")
			return nil
		})

		must.NoError(t, err)
		test.False(t, acquired)
		test.NoError(t, mock.ExpectationsWereMet())
	})

	T.Run("fn returning ErrLockNotAcquired is not mistaken for contention", func(t *testing.T) {
		t.Parallel()

		client, mock := buildSqlmockClient(t)
		s := newTestScopedLocker(t, client)

		mock.ExpectBegin()
		mock.ExpectQuery(tryXactLockPattern).WithArgs(sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"pg_try_advisory_xact_lock"}).AddRow(true))
		mock.ExpectRollback()

		acquired, err := s.TryWithLock(t.Context(), "chore", func(context.Context) error {
			return distributedlock.ErrLockNotAcquired
		})

		test.True(t, acquired)
		test.ErrorIs(t, err, distributedlock.ErrLockNotAcquired)
		test.NoError(t, mock.ExpectationsWereMet())
	})

	T.Run("rejects an empty key", func(t *testing.T) {
		t.Parallel()

		client, _ := buildSqlmockClient(t)
		s := newTestScopedLocker(t, client)

		acquired, err := s.TryWithLock(t.Context(), "", func(context.Context) error { return nil })
		test.ErrorIs(t, err, distributedlock.ErrEmptyKey)
		test.False(t, acquired)
	})

	T.Run("open circuit breaker refuses", func(t *testing.T) {
		t.Parallel()

		client, _ := buildSqlmockClient(t)
		cb := &cbmock.CircuitBreakerMock{
			CannotProceedFunc: func() bool { return true },
		}

		s, err := NewPostgresScopedLocker(&Config{}, client, cb)
		must.NoError(t, err)

		acquired, err := s.TryWithLock(t.Context(), "chore", func(context.Context) error {
			t.Fatal("fn must not run with an open breaker")
			return nil
		})
		test.Error(t, err)
		test.False(t, acquired)
	})

	T.Run("a failed probe trips the breaker", func(t *testing.T) {
		t.Parallel()

		client, mock := buildSqlmockClient(t)
		var failed int
		cb := &cbmock.CircuitBreakerMock{
			CannotProceedFunc: func() bool { return false },
			FailedFunc:        func() { failed++ },
		}

		s, err := NewPostgresScopedLocker(&Config{}, client, cb)
		must.NoError(t, err)

		mock.ExpectBegin()
		mock.ExpectQuery(tryXactLockPattern).WithArgs(sqlmock.AnyArg()).WillReturnError(errors.New("connection reset"))
		mock.ExpectRollback()

		acquired, err := s.TryWithLock(t.Context(), "chore", func(context.Context) error {
			t.Fatal("fn must not run when the probe never answered")
			return nil
		})

		test.Error(t, err)
		test.False(t, acquired)
		test.EqOp(t, 1, failed)
		test.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestPostgresScopedLocker_Container(T *testing.T) {
	T.Parallel()

	runWithContainerBackedPostgres(T, func(client *testDBClient) {
		T.Run("mutual exclusion across concurrent holders", func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			s := newTestScopedLocker(t, client)
			key := "scoped_mutex_" + identifiers.New()

			holding := make(chan struct{})
			releaseHold := make(chan struct{})
			holderDone := make(chan error, 1)

			go func() {
				holderDone <- s.WithLock(ctx, key, func(context.Context) error {
					close(holding)
					<-releaseHold
					return nil
				})
			}()

			<-holding

			// While held, TryWithLock must refuse.
			acquired, err := s.TryWithLock(ctx, key, func(context.Context) error { return nil })
			must.NoError(t, err)
			test.False(t, acquired)

			close(releaseHold)
			must.NoError(t, <-holderDone)

			// Once released, TryWithLock succeeds.
			acquired, err = s.TryWithLock(ctx, key, func(context.Context) error { return nil })
			must.NoError(t, err)
			test.True(t, acquired)
		})

		T.Run("WithLock queues behind a holder and proceeds when released", func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			s := newTestScopedLocker(t, client)
			key := "scoped_queue_" + identifiers.New()

			holding := make(chan struct{})
			releaseHold := make(chan struct{})
			holderDone := make(chan error, 1)
			waiterDone := make(chan error, 1)

			go func() {
				holderDone <- s.WithLock(ctx, key, func(context.Context) error {
					close(holding)
					<-releaseHold
					return nil
				})
			}()

			<-holding

			go func() {
				waiterDone <- s.WithLock(ctx, key, func(context.Context) error { return nil })
			}()

			// The waiter is queued server-side; releasing the holder lets it through.
			close(releaseHold)
			must.NoError(t, <-holderDone)

			select {
			case err := <-waiterDone:
				must.NoError(t, err)
			case <-time.After(30 * time.Second):
				t.Fatal("queued WithLock never acquired after release")
			}
		})

		T.Run("a panicking fn releases the lock", func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			s := newTestScopedLocker(t, client)
			key := "scoped_panic_" + identifiers.New()

			func() {
				defer func() {
					must.NotNil(t, recover())
				}()

				_ = s.WithLock(ctx, key, func(context.Context) error {
					panic("kaboom")
				})
			}()

			acquired, err := s.TryWithLock(ctx, key, func(context.Context) error { return nil })
			must.NoError(t, err)
			test.True(t, acquired)
		})
	})
}
