package dataprivacy

import (
	"testing"
	"time"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestServiceConfig(T *testing.T) {
	T.Parallel()

	T.Run("fills defaults", func(t *testing.T) {
		t.Parallel()

		cfg := &ServiceConfig{}
		cfg.EnsureDefaults()

		test.EqOp(t, DefaultResponseWindow, cfg.ExportResponseWindow)
		test.EqOp(t, DefaultResponseWindow, cfg.ErasureResponseWindow)
		test.EqOp(t, DefaultSignedURLTTL, cfg.SignedURLTTL)

		// Zero by default: erasures are queued on submission and Confirm is
		// never needed unless an operator asks for it.
		test.EqOp(t, time.Duration(0), cfg.ConfirmationWindow)

		test.NoError(t, cfg.ValidateWithContext(t.Context()))
	})

	T.Run("leaves configured values alone", func(t *testing.T) {
		t.Parallel()

		cfg := &ServiceConfig{
			ExportResponseWindow:  time.Hour,
			ErasureResponseWindow: 2 * time.Hour,
			SignedURLTTL:          time.Minute,
			ConfirmationWindow:    72 * time.Hour,
		}
		cfg.EnsureDefaults()

		test.EqOp(t, time.Hour, cfg.ExportResponseWindow)
		test.EqOp(t, 2*time.Hour, cfg.ErasureResponseWindow)
		test.EqOp(t, time.Minute, cfg.SignedURLTTL)
		test.EqOp(t, 72*time.Hour, cfg.ConfirmationWindow)
	})

	T.Run("selects the window per request type", func(t *testing.T) {
		t.Parallel()

		cfg := &ServiceConfig{ExportResponseWindow: time.Hour, ErasureResponseWindow: 2 * time.Hour}

		test.EqOp(t, time.Hour, cfg.responseWindow(RequestExport))
		test.EqOp(t, 2*time.Hour, cfg.responseWindow(RequestErasure))
	})
}

func TestWorkerConfig(T *testing.T) {
	T.Parallel()

	T.Run("fills defaults", func(t *testing.T) {
		t.Parallel()

		cfg := &WorkerConfig{}
		cfg.EnsureDefaults()

		test.EqOp(t, DefaultPollInterval, cfg.PollInterval)
		test.EqOp(t, DefaultBatchSize, cfg.BatchSize)
		test.EqOp(t, DefaultConcurrency, cfg.Concurrency)
		test.EqOp(t, DefaultCollectorConcurrency, cfg.CollectorConcurrency)
		test.EqOp(t, DefaultCollectorTimeout, cfg.CollectorTimeout)
		test.EqOp(t, DefaultFulfillmentTimeout, cfg.FulfillmentTimeout)
		test.EqOp(t, DefaultLeaseDuration, cfg.LeaseDuration)
		test.EqOp(t, DefaultArtifactTTL, cfg.ArtifactTTL)
		test.EqOp(t, DefaultArtifactPathPrefix, cfg.ArtifactPathPrefix)
		test.EqOp(t, DefaultMaxDocumentBytes, cfg.MaxDocumentBytes)

		test.NoError(t, cfg.ValidateWithContext(t.Context()))
	})

	T.Run("the default lease outlasts the default fulfillment timeout", func(t *testing.T) {
		t.Parallel()

		cfg := &WorkerConfig{}
		cfg.EnsureDefaults()

		// Otherwise a request would become claimable by a second worker while
		// the first was still fulfilling it.
		test.Greater(t, cfg.FulfillmentTimeout, cfg.LeaseDuration)
	})

	T.Run("rejects a lease that does not outlast the fulfillment timeout", func(t *testing.T) {
		t.Parallel()

		cfg := &WorkerConfig{FulfillmentTimeout: time.Hour, LeaseDuration: time.Minute}
		cfg.EnsureDefaults()

		err := cfg.ValidateWithContext(t.Context())
		must.Error(t, err)
		test.StrContains(t, err.Error(), "must exceed fulfillment timeout")
	})
}

func TestSweeperConfig(T *testing.T) {
	T.Parallel()

	T.Run("fills defaults", func(t *testing.T) {
		t.Parallel()

		cfg := &SweeperConfig{}
		cfg.EnsureDefaults()

		test.EqOp(t, DefaultRequestRetention, cfg.RequestRetention)
		test.EqOp(t, DefaultSweepBatchSize, cfg.BatchSize)
		test.False(t, cfg.DisableReap)

		test.NoError(t, cfg.ValidateWithContext(t.Context()))
	})
}

func TestSubject(T *testing.T) {
	T.Parallel()

	T.Run("requires an ID", func(t *testing.T) {
		t.Parallel()

		test.ErrorIs(t, Subject{}.validate(), ErrEmptySubjectID)
		test.NoError(t, Subject{ID: "user-1"}.validate())
	})
}

func TestTruncate(T *testing.T) {
	T.Parallel()

	T.Run("leaves a short string alone", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, "short", truncate("short", 100))
	})

	T.Run("cuts without splitting a rune", func(t *testing.T) {
		t.Parallel()

		// A truncated error still has to be valid UTF-8 or it will not store.
		cut := truncate("aa£", 3)

		test.EqOp(t, "aa", cut)
		test.True(t, utf8ValidString(cut))
	})

	T.Run("renders a nil error as empty", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, "", truncateError(nil))
	})
}

// utf8ValidString is a tiny indirection so the assertion above reads as an
// assertion rather than as a call into unicode/utf8.
func utf8ValidString(s string) bool {
	for _, r := range s {
		if r == '�' {
			return false
		}
	}

	return true
}
