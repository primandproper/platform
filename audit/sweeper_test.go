package audit

import (
	"context"
	"testing"
	"time"

	"github.com/primandproper/platform-go/v10/database"
	"github.com/primandproper/platform-go/v10/database/dialect"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
	"github.com/shoenig/test/wait"
)

// sweeperFor builds a Sweeper over the stub clock with the supplied config
// overrides applied.
func sweeperFor(t *testing.T, client database.Client, c *stubClock, mutate func(*SweeperConfig)) *Sweeper {
	t.Helper()

	cfg := &SweeperConfig{Dialect: dialect.SQLite, Retention: time.Hour}
	if mutate != nil {
		mutate(cfg)
	}

	s, err := NewSweeper(t.Context(), cfg, client, WithSweeperClock(c))
	must.NoError(t, err)

	return s
}

func TestSweeperConfig(T *testing.T) {
	T.Parallel()

	T.Run("EnsureDefaults fills every knob", func(t *testing.T) {
		t.Parallel()

		cfg := &SweeperConfig{Dialect: dialect.SQLite}
		cfg.EnsureDefaults()

		test.EqOp(t, DefaultTablePrefix, cfg.TablePrefix)
		test.EqOp(t, DefaultRetention, cfg.Retention)
		test.EqOp(t, DefaultSweepInterval, cfg.SweepInterval)
		test.EqOp(t, DefaultSweepBatchSize, cfg.BatchSize)
		test.EqOp(t, DefaultSweepScopeLimit, cfg.ScopeLimit)
	})

	T.Run("rejects an unsupported dialect", func(t *testing.T) {
		t.Parallel()

		cfg := &SweeperConfig{Dialect: "cassandra"}
		cfg.EnsureDefaults()

		test.Error(t, cfg.ValidateWithContext(t.Context()))
	})

	T.Run("rejects an unsafe table prefix", func(t *testing.T) {
		t.Parallel()

		cfg := &SweeperConfig{Dialect: dialect.SQLite, TablePrefix: "audit-"}
		cfg.EnsureDefaults()

		test.Error(t, cfg.ValidateWithContext(t.Context()))
	})

	T.Run("refuses a retention window shorter than an hour", func(t *testing.T) {
		t.Parallel()

		cfg := &SweeperConfig{Dialect: dialect.SQLite}
		cfg.EnsureDefaults()
		cfg.Retention = time.Second

		// A misplaced unit on a compliance parameter should stop a process from
		// starting, not quietly empty the table.
		test.Error(t, cfg.ValidateWithContext(t.Context()))
	})
}

func TestNewSweeper(T *testing.T) {
	T.Parallel()

	T.Run("rejects a nil config", func(t *testing.T) {
		t.Parallel()

		_, err := NewSweeper(t.Context(), nil, newTestClient(t))
		test.Error(t, err)
	})

	T.Run("rejects a nil client", func(t *testing.T) {
		t.Parallel()

		_, err := NewSweeper(t.Context(), &SweeperConfig{Dialect: dialect.SQLite}, nil)
		test.ErrorIs(t, err, ErrNilDatabaseClient)
	})
}

func TestSweeper_Sweep(T *testing.T) {
	T.Parallel()

	T.Run("prunes past the window and leaves the rest verifiable", func(t *testing.T) {
		t.Parallel()

		client := newTestClient(t)
		c := newStubClock()
		recorder := newTestRecorder(t, c)
		reader := newTestReader(t, client)

		first := entryFor("acct_1", "r1")
		record(t, client, recorder, first)

		c.advance(2 * time.Hour)
		second := entryFor("acct_1", "r2")
		record(t, client, recorder, second)

		c.advance(2 * time.Hour)
		record(t, client, recorder, entryFor("acct_1", "r3"))

		sweeper := sweeperFor(t, client, c, func(cfg *SweeperConfig) { cfg.Retention = 3 * time.Hour })

		test.EqOp(t, int64(1), sweeper.Sweep(t.Context()))
		test.EqOp(t, 2, countRows(t, client, "audit_log_entries", "1=1"))

		// The watermark the sweep left behind is what the oldest survivor is
		// anchored against; without it this would read as a deleted entry.
		result, err := reader.Verify(t.Context(), "acct_1", time.Time{}, time.Time{})
		must.NoError(t, err)
		test.True(t, result.Intact())
		test.EqOp(t, 2, result.Checked)

		_, err = reader.Get(t.Context(), first.ID)
		test.ErrorIs(t, err, ErrEntryNotFound)
	})

	T.Run("leaves entries inside the window alone", func(t *testing.T) {
		t.Parallel()

		client := newTestClient(t)
		c := newStubClock()
		recorder := newTestRecorder(t, c)

		record(t, client, recorder, entryFor("acct_1", "r1"))
		c.advance(time.Minute)

		sweeper := sweeperFor(t, client, c, func(cfg *SweeperConfig) { cfg.Retention = time.Hour })

		test.EqOp(t, int64(0), sweeper.Sweep(t.Context()))
		test.EqOp(t, 1, countRows(t, client, "audit_log_entries", "1=1"))
	})

	T.Run("does nothing on an empty log", func(t *testing.T) {
		t.Parallel()

		client := newTestClient(t)
		c := newStubClock()

		sweeper := sweeperFor(t, client, c, nil)

		test.EqOp(t, int64(0), sweeper.Sweep(t.Context()))
	})

	T.Run("caps one pass at the batch size", func(t *testing.T) {
		t.Parallel()

		client := newTestClient(t)
		c := newStubClock()
		recorder := newTestRecorder(t, c)
		reader := newTestReader(t, client)

		for i := range 5 {
			record(t, client, recorder, entryFor("acct_1", string(rune('a'+i))))
		}

		c.advance(4 * time.Hour)

		sweeper := sweeperFor(t, client, c, func(cfg *SweeperConfig) {
			cfg.Retention = time.Hour
			cfg.BatchSize = 2
		})

		test.EqOp(t, int64(2), sweeper.Sweep(t.Context()))
		test.EqOp(t, 3, countRows(t, client, "audit_log_entries", "1=1"))

		// Still contiguous, still anchored: a batched sweep is several prefix
		// prunes, never a hole.
		result, err := reader.Verify(t.Context(), "acct_1", time.Time{}, time.Time{})
		must.NoError(t, err)
		test.True(t, result.Intact())

		test.EqOp(t, int64(2), sweeper.Sweep(t.Context()))
		test.EqOp(t, 1, countRows(t, client, "audit_log_entries", "1=1"))

		result, err = reader.Verify(t.Context(), "acct_1", time.Time{}, time.Time{})
		must.NoError(t, err)
		test.True(t, result.Intact())
	})

	T.Run("sweeps every scope", func(t *testing.T) {
		t.Parallel()

		client := newTestClient(t)
		c := newStubClock()
		recorder := newTestRecorder(t, c)

		record(t, client, recorder, entryFor("acct_1", "r1"), entryFor("acct_2", "r1"))
		c.advance(4 * time.Hour)

		sweeper := sweeperFor(t, client, c, func(cfg *SweeperConfig) { cfg.Retention = time.Hour })

		test.EqOp(t, int64(2), sweeper.Sweep(t.Context()))
		test.EqOp(t, 0, countRows(t, client, "audit_log_entries", "1=1"))
	})

	T.Run("lets a chain continue after its entries are all pruned", func(t *testing.T) {
		t.Parallel()

		client := newTestClient(t)
		c := newStubClock()
		recorder := newTestRecorder(t, c)
		reader := newTestReader(t, client)

		first := entryFor("acct_1", "r1")
		record(t, client, recorder, first)

		c.advance(4 * time.Hour)

		sweeper := sweeperFor(t, client, c, func(cfg *SweeperConfig) { cfg.Retention = time.Hour })
		test.EqOp(t, int64(1), sweeper.Sweep(t.Context()))

		// The chain row outlives the entries, so the next write continues the
		// chain rather than restarting at a position already used.
		next := entryFor("acct_1", "r2")
		record(t, client, recorder, next)

		test.EqOp(t, int64(1), next.Seq)
		test.EqOp(t, first.Hash, next.PrevHash)

		result, err := reader.Verify(t.Context(), "acct_1", time.Time{}, time.Time{})
		must.NoError(t, err)
		test.True(t, result.Intact())
	})
}

func TestSweeper_RunAndClose(T *testing.T) {
	T.Parallel()

	T.Run("stops on Close, and stops twice safely", func(t *testing.T) {
		t.Parallel()

		client := newTestClient(t)
		c := newStubClock()

		// The interval is long enough that the ticker never fires: this asserts
		// the lifecycle, not the cadence.
		sweeper := sweeperFor(t, client, c, func(cfg *SweeperConfig) { cfg.SweepInterval = time.Hour })

		go sweeper.Run()

		must.NoError(t, sweeper.Close(t.Context()))
		must.NoError(t, sweeper.Close(t.Context()))
	})

	T.Run("sweeps on the tick", func(t *testing.T) {
		t.Parallel()

		client := newTestClient(t)
		c := newStubClock()
		recorder := newTestRecorder(t, c)

		record(t, client, recorder, entryFor("acct_1", "r1"))
		c.advance(4 * time.Hour)

		// One second, because that is the configured floor — a sweep interval
		// below it is refused, so this is as fast as the tick path can be
		// exercised honestly.
		sweeper := sweeperFor(t, client, c, func(cfg *SweeperConfig) {
			cfg.Retention = time.Hour
			cfg.SweepInterval = time.Second
		})

		go sweeper.Run()

		// The ticker is real (see stubClock), so this waits on the loop rather
		// than on a fixed sleep.
		must.Wait(t, wait.InitialSuccess(
			wait.BoolFunc(func() bool {
				return countRows(t, client, "audit_log_entries", "1=1") == 0
			}),
			wait.Timeout(15*time.Second),
			wait.Gap(10*time.Millisecond),
		))

		must.NoError(t, sweeper.Close(t.Context()))
	})

	T.Run("reports a Close that outlives its context", func(t *testing.T) {
		t.Parallel()

		client := newTestClient(t)
		c := newStubClock()

		sweeper := sweeperFor(t, client, c, func(cfg *SweeperConfig) { cfg.SweepInterval = time.Hour })

		// Never started, so nothing will ever close the done channel and Close
		// can only end by giving up on its deadline.
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		test.Error(t, sweeper.Close(ctx))
	})
}
