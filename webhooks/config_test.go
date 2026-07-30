package webhooks

import (
	"testing"
	"time"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestWorkerConfig_EnsureDefaults(T *testing.T) {
	T.Parallel()

	T.Run("fills every unset knob", func(t *testing.T) {
		t.Parallel()

		cfg := &WorkerConfig{}
		cfg.EnsureDefaults()

		test.EqOp(t, DefaultBatchSize, cfg.BatchSize)
		test.EqOp(t, DefaultConcurrency, cfg.Concurrency)
		test.EqOp(t, DefaultPollInterval, cfg.PollInterval)
		test.EqOp(t, DefaultLeaseDuration, cfg.LeaseDuration)
		test.EqOp(t, DefaultRequestTimeout, cfg.RequestTimeout)
		test.EqOp(t, DefaultCircuitOpenRetryDelay, cfg.CircuitOpenRetryDelay)
		test.EqOp(t, DefaultRetention, cfg.Retention)
		test.EqOp(t, DefaultReapInterval, cfg.ReapInterval)
		test.EqOp(t, DefaultReapBatchSize, cfg.ReapBatchSize)
		test.EqOp(t, DefaultUserAgent, cfg.UserAgent)
		test.True(t, cfg.Backoff.MaxAttempts > 0)
	})

	T.Run("leaves explicit values alone", func(t *testing.T) {
		t.Parallel()

		cfg := &WorkerConfig{
			BatchSize:      7,
			Concurrency:    3,
			RequestTimeout: 2 * time.Second,
			UserAgent:      "acme-hooks/2",
		}
		cfg.EnsureDefaults()

		test.EqOp(t, 7, cfg.BatchSize)
		test.EqOp(t, 3, cfg.Concurrency)
		test.EqOp(t, 2*time.Second, cfg.RequestTimeout)
		test.EqOp(t, "acme-hooks/2", cfg.UserAgent)
	})

	// The defaults must satisfy their own validation, or a caller who configures
	// nothing cannot construct a Worker.
	T.Run("the defaults validate", func(t *testing.T) {
		t.Parallel()

		cfg := &WorkerConfig{}
		cfg.EnsureDefaults()

		test.NoError(t, cfg.ValidateWithContext(t.Context()))
	})
}

func TestWorkerConfig_ValidateWithContext(T *testing.T) {
	T.Parallel()

	// The lease has to outlast the request it covers. A shorter one expires
	// mid-flight, a second worker reclaims the dispatch, and the subscriber gets
	// the same payload twice from two workers at once.
	T.Run("rejects a lease that does not outlast a request", func(t *testing.T) {
		t.Parallel()

		cfg := &WorkerConfig{}
		cfg.EnsureDefaults()
		cfg.RequestTimeout = cfg.LeaseDuration

		err := cfg.ValidateWithContext(t.Context())
		must.Error(t, err)
		test.StrContains(t, err.Error(), ErrLeaseTooShort.Error())
	})

	T.Run("accepts a lease longer than a request", func(t *testing.T) {
		t.Parallel()

		cfg := &WorkerConfig{
			RequestTimeout: 10 * time.Second,
			LeaseDuration:  11 * time.Second,
		}
		cfg.EnsureDefaults()

		test.NoError(t, cfg.ValidateWithContext(t.Context()))
	})

	T.Run("rejects a zero config", func(t *testing.T) {
		t.Parallel()

		test.Error(t, (&WorkerConfig{}).ValidateWithContext(t.Context()))
	})
}
