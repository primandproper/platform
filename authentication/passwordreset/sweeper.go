package passwordreset

import (
	"context"
	"fmt"
	"time"

	platformerrors "github.com/primandproper/platform-go/v13/errors"
	"github.com/primandproper/platform-go/v13/observability/metrics"
)

// sweptKey is this store's one observability key that names no row.
const sweptKey = "password_reset.swept"

// backgroundSweepFailure is what the background loop logs when a sweep fails.
// It is a constant because a test asserts on it: the loop's only effect is this
// line, so a loop that stopped emitting it would otherwise fail silently.
const backgroundSweepFailure = "background sweep of expired password reset token rows failed"

// Sweep removes every row whose deadline has passed, reporting how many it
// removed.
//
// It is not what makes a token expire — Verify and Consume refuse a row past its
// deadline, so a row this has not reached yet is already dead. What it does is
// stop the table growing by a row for every password anybody ever forgot,
// including the requests nobody followed up, which are the ones no redemption
// ever removes.
//
// It spans every scope, deliberately, and is the one method here that does. A
// scheduler reclaims one table for the whole deployment, and a per-scope sweep
// would need a list of scopes nothing maintains. It answers no consumer read —
// it returns a count, not rows — which is what keeps it inside the narrow
// exception the tenancy doctrine allows a component's own machinery.
//
// One statement, no batching. Token rows are small and the index on expires_at
// makes the delete proportional to what is actually dead rather than to the
// table.
func (s *SQLStore) Sweep(ctx context.Context) (int64, error) {
	ctx, op := s.o11y.Begin(ctx)
	defer op.End()

	query, args := buildSweep(s.dialect, s.table, s.clock.Now())

	result, err := s.db.Writer().ExecContext(ctx, query, args...)
	if err != nil {
		s.sweepErrorsCounter.Add(ctx, 1)

		return 0, op.Error(err, "sweeping expired password reset token rows")
	}

	swept, err := result.RowsAffected()
	if err != nil {
		s.sweepErrorsCounter.Add(ctx, 1)

		return 0, op.Error(err, "counting swept password reset token rows")
	}

	s.sweptCounter.Add(ctx, swept)
	op.Set(sweptKey, swept)

	return swept, nil
}

// sweepEvery sweeps on every tick until ctx is done.
//
// Ticks come from the injected clock, so inside a testing/synctest bubble the
// sweeper advances with the bubble's fake time and needs no test double.
func (s *SQLStore) sweepEvery(ctx context.Context, interval time.Duration) {
	ticker := s.clock.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.Chan():
			// Logged rather than returned: nothing is waiting on this
			// goroutine, and a sweep that fails is a table that grows for
			// another interval, not a reset that misbehaves.
			//
			// The message is the loop's own rather than Sweep's, which has
			// already recorded the failure against its span. What this line
			// adds is which sweep it was: a background one that nobody asked
			// for and nobody will retry, as opposed to a scheduler's call that
			// reported an error to a caller who can.
			if _, err := s.Sweep(ctx); err != nil {
				s.o11y.Logger().Error(backgroundSweepFailure, err)
			}
		}
	}
}

// newSweepInstruments builds the two counters the sweeper owns. They live here
// rather than beside the reset flow because nothing above this layer knows a
// sweep happened.
func newSweepInstruments(provider metrics.Provider) (swept, errs metrics.Int64Counter, err error) {
	mp := metrics.EnsureMetricsProvider(provider)

	if swept, err = mp.NewInt64Counter(fmt.Sprintf("%s_rows_swept", serviceName)); err != nil {
		return nil, nil, platformerrors.Wrap(err, "creating swept password reset token rows counter")
	}

	if errs, err = mp.NewInt64Counter(fmt.Sprintf("%s_sweep_errors", serviceName)); err != nil {
		return nil, nil, platformerrors.Wrap(err, "creating password reset token sweep errors counter")
	}

	return swept, errs, nil
}
