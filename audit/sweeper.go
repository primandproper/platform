package audit

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/primandproper/platform-go/v9/clock"
	"github.com/primandproper/platform-go/v9/database"
	"github.com/primandproper/platform-go/v9/database/dialect"
	platformerrors "github.com/primandproper/platform-go/v9/errors"
	"github.com/primandproper/platform-go/v9/observability"
	"github.com/primandproper/platform-go/v9/observability/logging"
	"github.com/primandproper/platform-go/v9/observability/metrics"
	"github.com/primandproper/platform-go/v9/observability/tracing"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

const (
	// DefaultRetention is how long entries are kept before a sweep may remove
	// them. Seven years is the default because that is the window the
	// regulations that ask for an audit log in the first place tend to name, and
	// a default that quietly deletes evidence someone was required to keep is a
	// worse failure than a table that grew larger than expected.
	DefaultRetention = 7 * 365 * 24 * time.Hour
	// DefaultSweepInterval is how often the sweeper runs.
	DefaultSweepInterval = time.Hour
	// DefaultSweepBatchSize caps how many entries one sweep removes from one
	// scope, so a long-neglected log is trimmed over several passes instead of
	// one DELETE that holds locks for minutes.
	DefaultSweepBatchSize = 1000
	// DefaultSweepScopeLimit caps how many scopes one sweep tick visits.
	DefaultSweepScopeLimit = 100
)

// SweeperConfig configures a Sweeper.
type SweeperConfig struct {
	// Dialect selects the SQL emitted; it must match the database.Client.
	Dialect dialect.Dialect `env:"DIALECT" json:"dialect" yaml:"dialect"`
	// TablePrefix is the prefix the audit tables carry. Defaults to
	// DefaultTablePrefix, and must match the Recorder's.
	TablePrefix string `env:"TABLE_PREFIX" json:"tablePrefix" yaml:"tablePrefix"`
	// Retention is how long an entry is kept. Defaults to DefaultRetention.
	Retention time.Duration `env:"RETENTION" json:"retention" yaml:"retention"`
	// SweepInterval is how often the sweeper runs.
	SweepInterval time.Duration `env:"SWEEP_INTERVAL" json:"sweepInterval" yaml:"sweepInterval"`
	// BatchSize caps how many entries one sweep removes from one scope.
	BatchSize int `env:"BATCH_SIZE" json:"batchSize" yaml:"batchSize"`
	// ScopeLimit caps how many scopes one sweep tick visits.
	ScopeLimit int `env:"SCOPE_LIMIT" json:"scopeLimit" yaml:"scopeLimit"`
}

var _ validation.ValidatableWithContext = (*SweeperConfig)(nil)

// EnsureDefaults fills unset knobs with the package defaults.
func (cfg *SweeperConfig) EnsureDefaults() {
	if cfg.TablePrefix == "" {
		cfg.TablePrefix = DefaultTablePrefix
	}
	if cfg.Retention <= 0 {
		cfg.Retention = DefaultRetention
	}
	if cfg.SweepInterval <= 0 {
		cfg.SweepInterval = DefaultSweepInterval
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = DefaultSweepBatchSize
	}
	if cfg.ScopeLimit <= 0 {
		cfg.ScopeLimit = DefaultSweepScopeLimit
	}
}

// ValidateWithContext validates a SweeperConfig.
func (cfg *SweeperConfig) ValidateWithContext(ctx context.Context) error {
	return validation.ValidateStructWithContext(ctx, cfg,
		validation.Field(&cfg.Dialect, validation.Required, validation.By(func(any) error {
			if !cfg.Dialect.Valid() {
				return platformerrors.Wrapf(dialect.ErrUnsupported, "audit dialect %q", cfg.Dialect)
			}

			return nil
		})),
		validation.Field(&cfg.TablePrefix, validation.By(func(any) error {
			if !validPrefix.MatchString(cfg.TablePrefix) {
				return platformerrors.Wrapf(ErrInvalidTablePrefix, "audit table prefix %q", cfg.TablePrefix)
			}

			return nil
		})),
		// An hour, not a second. Retention on an audit log is a compliance
		// parameter, and a misplaced unit that would otherwise mean "keep
		// nothing" is worth refusing to start over.
		validation.Field(&cfg.Retention, validation.Required, validation.Min(time.Hour)),
		validation.Field(&cfg.SweepInterval, validation.Required, validation.Min(time.Second)),
		validation.Field(&cfg.BatchSize, validation.Required, validation.Min(1)),
		validation.Field(&cfg.ScopeLimit, validation.Required, validation.Min(1)),
	)
}

// Sweeper enforces retention. It owns a goroutine started by Run and stopped by
// Close.
//
// It deletes, which sits oddly in a package about entries being undeletable —
// so it is careful about it in two ways. It only ever removes a prefix of a
// scope's chain, never a row from the middle, so the survivors stay contiguous
// and verifiable against each other. And it records the hash of the last entry
// it removed as that scope's prune watermark, so the oldest surviving entry
// still has something to link to and Verify can tell retention's gap from a
// deletion.
type Sweeper struct {
	client database.Client
	clock  clock.Clock
	o11y   observability.Observer
	logger logging.Logger
	tables *tables

	stop chan struct{}
	done chan struct{}

	prunedCounter metrics.Int64Counter
	errorsCounter metrics.Int64Counter
	sweepHist     metrics.Float64Histogram

	tracerProvider  tracing.TracerProvider
	metricsProvider metrics.Provider

	cfg SweeperConfig

	stopOnce sync.Once
}

// NewSweeper builds a Sweeper. It does not start it; call Run.
//
// ctx is used to validate the config and is not retained — Run takes its own.
func NewSweeper(ctx context.Context, cfg *SweeperConfig, client database.Client, opts ...SweeperOption) (*Sweeper, error) {
	if cfg == nil {
		return nil, platformerrors.Wrap(platformerrors.ErrNilInputParameter, "nil audit sweeper config")
	}
	if client == nil {
		return nil, ErrNilDatabaseClient
	}

	cfg.EnsureDefaults()

	s := &Sweeper{
		cfg:    *cfg,
		client: client,
		clock:  clock.NewClock(),
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(s)
		}
	}

	if err := s.cfg.ValidateWithContext(ctx); err != nil {
		return nil, platformerrors.Wrap(err, "validating audit sweeper config")
	}

	s.tables = newTables(s.cfg.TablePrefix)

	s.o11y = observability.NewObserver(serviceName, s.logger, s.tracerProvider)
	s.logger = s.o11y.Logger()

	mp := metrics.EnsureMetricsProvider(s.metricsProvider)

	var err error
	if s.prunedCounter, err = mp.NewInt64Counter(fmt.Sprintf("%s_entries_pruned", serviceName)); err != nil {
		return nil, platformerrors.Wrap(err, "creating entries pruned counter")
	}
	if s.errorsCounter, err = mp.NewInt64Counter(fmt.Sprintf("%s_sweep_errors", serviceName)); err != nil {
		return nil, platformerrors.Wrap(err, "creating sweep errors counter")
	}
	if s.sweepHist, err = mp.NewFloat64Histogram(fmt.Sprintf("%s_sweep_latency_ms", serviceName)); err != nil {
		return nil, platformerrors.Wrap(err, "creating sweep latency histogram")
	}

	return s, nil
}

// Run is the sweep loop. Like outbox.Relay.Run it takes no context: tied to a
// server context it would stop on shutdown, which is harmless here but would
// make the lifecycle differ from every other background loop in this module for
// no reason. The owner calls Close.
//
// Run returns only after Close.
func (s *Sweeper) Run() {
	defer close(s.done)

	ctx := context.Background()

	ticker := s.clock.NewTicker(s.cfg.SweepInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stop:
			return
		case <-ticker.Chan():
			s.Sweep(ctx)
		}
	}
}

// Close stops the sweeper and waits for the in-flight sweep to finish. Safe to
// call more than once.
func (s *Sweeper) Close(ctx context.Context) error {
	_, op := s.o11y.Begin(ctx)
	defer op.End()

	s.stopOnce.Do(func() { close(s.stop) })

	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		return op.Error(ctx.Err(), "waiting for audit sweeper to finish")
	}
}

// Sweep prunes one batch from each scope holding entries past the retention
// window, and reports how many entries it removed.
//
// It is exported so an operator can force a sweep, and so tests can drive one
// without a ticker. Errors are logged and counted rather than returned: the
// next tick retries, and a scope that fails does not stop the others.
func (s *Sweeper) Sweep(ctx context.Context) int64 {
	before := s.clock.Now().UTC().Add(-s.cfg.Retention).Truncate(time.Microsecond)

	scopes, err := s.prunableScopes(ctx, before)
	if err != nil {
		s.errorsCounter.Add(ctx, 1)
		s.logger.Error("listing prunable audit scopes", err)

		return 0
	}

	if len(scopes) == 0 {
		return 0
	}

	startTime := time.Now()
	defer func() {
		s.sweepHist.Record(ctx, float64(time.Since(startTime).Milliseconds()))
	}()

	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(retentionCutoffKey, before),
		observability.WithValue(scopeCountKey, len(scopes)),
	)
	defer op.End()

	var pruned int64

	for _, scope := range scopes {
		n, sweepErr := s.sweepScope(ctx, scope, before)
		if sweepErr != nil {
			s.errorsCounter.Add(ctx, 1)
			op.Acknowledge(sweepErr, "pruning audit entries for scope %q", scope)

			continue
		}

		pruned += n
	}

	op.Set(prunedKey, pruned)
	s.prunedCounter.Add(ctx, pruned)

	return pruned
}

// prunableScopes lists the scopes holding anything older than the cutoff.
func (s *Sweeper) prunableScopes(ctx context.Context, before time.Time) ([]string, error) {
	query, args := s.tables.buildSelectPrunableScopes(s.cfg.Dialect, before, s.cfg.ScopeLimit)

	rows, err := s.client.Reader().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, platformerrors.Wrap(err, "querying prunable audit scopes")
	}

	var scopes []string
	if err = scanRows(rows, func() error {
		var scope string
		if scanErr := rows.Scan(&scope); scanErr != nil {
			return scanErr
		}
		scopes = append(scopes, scope)

		return nil
	}); err != nil {
		return nil, platformerrors.Wrap(err, "scanning prunable audit scopes")
	}

	return scopes, nil
}

// sweepScope prunes one batch from one scope, in a single transaction so the
// deletion and the watermark that explains it cannot be separated. If they
// could, a crash between them would leave a gap that Verify — correctly — would
// report as a deletion.
func (s *Sweeper) sweepScope(ctx context.Context, scope string, before time.Time) (int64, error) {
	var pruned int64

	err := s.client.WithTransaction(ctx, func(q database.SQLQueryExecutor) error {
		boundary, ok, err := s.pruneBoundary(ctx, q, scope, before)
		if !ok || err != nil {
			return err
		}

		query, args := s.tables.buildSelectPruneTarget(s.cfg.Dialect, scope, boundary)

		var (
			targetSeq  int64
			targetHash string
		)
		if err = q.QueryRowContext(ctx, query, args...).Scan(&targetSeq, &targetHash); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}

			return platformerrors.Wrap(err, "reading audit prune target")
		}

		query, args = s.tables.buildDeletePruned(s.cfg.Dialect, scope, targetSeq)

		result, err := q.ExecContext(ctx, query, args...)
		if err != nil {
			return platformerrors.Wrap(err, "deleting aged audit entries")
		}

		if pruned, err = result.RowsAffected(); err != nil {
			return platformerrors.Wrap(err, "counting pruned audit entries")
		}

		// The watermark is what keeps the chain verifiable across the gap this
		// DELETE just made: the oldest surviving entry's PrevHash is checked
		// against it rather than against a row that no longer exists.
		query, args = s.tables.buildUpdateChainPruned(s.cfg.Dialect, scope, targetHash, targetSeq, s.clock.Now().UTC())
		if _, err = q.ExecContext(ctx, query, args...); err != nil {
			return platformerrors.Wrap(err, "recording audit prune watermark")
		}

		return nil
	})
	if err != nil {
		return 0, err
	}

	return pruned, nil
}

// pruneBoundary computes the highest position this sweep may remove from a
// scope, reporting false when there is nothing to do.
//
// Two bounds apply and the lower wins. The batch bound keeps one pass short.
// The correctness bound is the first entry that must survive the cutoff:
// pruning strictly below it is what guarantees the survivors remain a
// contiguous suffix, which deleting by timestamp alone would not — recorded_at
// comes from the recording process's clock and so is not perfectly ordered with
// respect to position across several processes.
func (s *Sweeper) pruneBoundary(
	ctx context.Context,
	q database.SQLQueryExecutor,
	scope string,
	before time.Time,
) (boundary int64, ok bool, err error) {
	query, args := s.tables.buildSelectPruneBounds(s.cfg.Dialect, scope, before)

	var oldest, firstToKeep sql.NullInt64
	if err = q.QueryRowContext(ctx, query, args...).Scan(&oldest, &firstToKeep); err != nil {
		return 0, false, platformerrors.Wrap(err, "reading audit prune bounds")
	}

	if !oldest.Valid {
		return 0, false, nil
	}

	boundary = oldest.Int64 + int64(s.cfg.BatchSize) - 1
	if firstToKeep.Valid && firstToKeep.Int64 <= boundary {
		boundary = firstToKeep.Int64 - 1
	}

	if boundary < oldest.Int64 {
		return 0, false, nil
	}

	return boundary, true, nil
}
