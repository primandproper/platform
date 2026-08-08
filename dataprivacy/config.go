package dataprivacy

import (
	"context"
	"time"

	platformerrors "github.com/primandproper/platform-go/v10/errors"
	retrycfg "github.com/primandproper/platform-go/v10/retry/config"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

const (
	// DefaultResponseWindow is how long a request may take before it is
	// overdue.
	//
	// Thirty days is GDPR's window; CCPA allows forty-five. The stricter of the
	// two is the default because a deadline that is too early produces a gauge
	// somebody looks at, and one that is too late produces a fine.
	DefaultResponseWindow = 30 * 24 * time.Hour

	// DefaultArtifactTTL is how long an export artifact survives before the
	// sweeper deletes it.
	//
	// Seven days. The artifact contains everything an application knows about a
	// person, and the single worst outcome available to this package is leaving
	// one in a bucket indefinitely. Long enough that somebody on holiday can
	// still fetch it; short enough that it is not a permanent object.
	DefaultArtifactTTL = 7 * 24 * time.Hour

	// DefaultSignedURLTTL is how long a download URL is valid. Minutes, not
	// days: the link is mailed to the subject and mail is not a confidential
	// channel, so the window in which an intercepted link is useful should be
	// the window in which somebody clicks it.
	DefaultSignedURLTTL = 15 * time.Minute

	// DefaultArtifactPathPrefix is the storage prefix artifacts are written
	// under.
	DefaultArtifactPathPrefix = "dataprivacy/exports"

	// DefaultPollInterval is how often the Worker looks for pending requests.
	DefaultPollInterval = 10 * time.Second

	// DefaultBatchSize is how many requests one Worker cycle claims. Small,
	// because one request is a fan-out over every registered domain and a batch
	// of fifty would be a thundering herd against the application's own
	// database.
	DefaultBatchSize = 5

	// DefaultConcurrency is how many claimed requests a Worker fulfills at once.
	DefaultConcurrency = 2

	// DefaultCollectorConcurrency is how many of one request's collectors run at
	// once.
	DefaultCollectorConcurrency = 4

	// DefaultCollectorTimeout bounds one collector. It exists so that one slow
	// domain costs its own section rather than the whole export — which is the
	// entire reason collection is per-key.
	DefaultCollectorTimeout = 30 * time.Second

	// DefaultFulfillmentTimeout bounds one whole request.
	DefaultFulfillmentTimeout = 10 * time.Minute

	// DefaultLeaseDuration is how long a claimed request stays leased. It must
	// exceed DefaultFulfillmentTimeout, or a second worker starts a request the
	// first is still fulfilling.
	DefaultLeaseDuration = 15 * time.Minute

	// DefaultMaxDocumentBytes caps the assembled export before it is written.
	//
	// A collector that answers a bad subject ID with its entire table is a bug
	// that presents as an out-of-memory kill in the worker, taking every other
	// in-flight request with it. Failing the one request loudly is better.
	DefaultMaxDocumentBytes int64 = 512 << 20 // 512 MiB

	// DefaultSweepInterval is the recommended cadence for running the Sweeper.
	//
	// It is a suggestion for the caller's scheduler, not something this package
	// acts on: the Sweeper has no ticker of its own and does one pass per call.
	// It was previously also a config field, which read as though setting it made
	// the Sweeper run on that interval — nothing ever did.
	DefaultSweepInterval = time.Hour

	// DefaultSweepBatchSize caps how much one sweep tick does.
	DefaultSweepBatchSize = 100

	// DefaultRequestRetention is how long a terminal request record is kept
	// before the sweeper reaps it.
	//
	// A record of a privacy request is itself personal data — it says that a
	// named person asked, and when — so keeping it forever is the mistake this
	// package would otherwise make on every consumer's behalf. Three years
	// outlasts any plausible dispute about whether a request was honored while
	// not being indefinite.
	DefaultRequestRetention = 3 * 365 * 24 * time.Hour
)

// ServiceConfig configures the request state machine's timings.
type ServiceConfig struct {
	// ExportResponseWindow is how long an export may take before it counts as
	// overdue. Defaults to DefaultResponseWindow.
	ExportResponseWindow time.Duration `env:"EXPORT_RESPONSE_WINDOW" json:"exportResponseWindow,omitempty" yaml:"exportResponseWindow,omitempty"`

	// ErasureResponseWindow is the same for erasures. Separate from the export
	// window because the jurisdictions that distinguish them give erasure the
	// longer one, and a single knob would force the stricter deadline onto both.
	ErasureResponseWindow time.Duration `env:"ERASURE_RESPONSE_WINDOW" json:"erasureResponseWindow,omitempty" yaml:"erasureResponseWindow,omitempty"`

	// ConfirmationWindow is how long an erasure waits for confirmation before
	// it is cancelled. Zero — the default — means erasures are queued on
	// submission and Confirm is never needed.
	//
	// Turning it on is the difference between an accidental erasure being a
	// support ticket and being unrecoverable. Regulation generally permits a
	// verification step, and the failure mode it prevents is the only one in
	// this package that cannot be undone.
	ConfirmationWindow time.Duration `env:"CONFIRMATION_WINDOW" json:"confirmationWindow,omitempty" yaml:"confirmationWindow,omitempty"`

	// SignedURLTTL is how long a download URL is valid. Defaults to
	// DefaultSignedURLTTL.
	SignedURLTTL time.Duration `env:"SIGNED_URL_TTL" json:"signedURLTTL,omitempty" yaml:"signedURLTTL,omitempty"`
}

var _ validation.ValidatableWithContext = (*ServiceConfig)(nil)

// EnsureDefaults fills unset knobs with the package defaults.
func (cfg *ServiceConfig) EnsureDefaults() {
	if cfg.ExportResponseWindow <= 0 {
		cfg.ExportResponseWindow = DefaultResponseWindow
	}
	if cfg.ErasureResponseWindow <= 0 {
		cfg.ErasureResponseWindow = DefaultResponseWindow
	}
	if cfg.SignedURLTTL <= 0 {
		cfg.SignedURLTTL = DefaultSignedURLTTL
	}
}

// ValidateWithContext validates a ServiceConfig.
func (cfg *ServiceConfig) ValidateWithContext(ctx context.Context) error {
	return validation.ValidateStructWithContext(ctx, cfg,
		validation.Field(&cfg.ExportResponseWindow, validation.Required),
		validation.Field(&cfg.ErasureResponseWindow, validation.Required),
		validation.Field(&cfg.SignedURLTTL, validation.Required),
	)
}

// responseWindow returns the deadline window for a request type.
func (cfg *ServiceConfig) responseWindow(t RequestType) time.Duration {
	if t == RequestErasure {
		return cfg.ErasureResponseWindow
	}

	return cfg.ExportResponseWindow
}

// WorkerConfig configures the fulfillment loop.
type WorkerConfig struct {
	// ArtifactPathPrefix is the storage prefix artifacts are written under.
	// Defaults to DefaultArtifactPathPrefix.
	ArtifactPathPrefix string `env:"ARTIFACT_PATH_PREFIX" json:"artifactPathPrefix,omitempty" yaml:"artifactPathPrefix,omitempty"`

	// Backoff schedules the retry of a request that failed for a reason worth
	// retrying.
	Backoff retrycfg.Config `env:",init" envPrefix:"BACKOFF_" json:"backoff,omitzero" yaml:"backoff,omitempty"`

	// PollInterval is how often the Worker looks for pending requests.
	PollInterval time.Duration `env:"POLL_INTERVAL" json:"pollInterval,omitempty" yaml:"pollInterval,omitempty"`

	// LeaseDuration is how long a claimed request stays leased. It must exceed
	// FulfillmentTimeout — see ValidateWithContext.
	LeaseDuration time.Duration `env:"LEASE_DURATION" json:"leaseDuration,omitempty" yaml:"leaseDuration,omitempty"`

	// FulfillmentTimeout bounds one whole request.
	FulfillmentTimeout time.Duration `env:"FULFILLMENT_TIMEOUT" json:"fulfillmentTimeout,omitempty" yaml:"fulfillmentTimeout,omitempty"`

	// CollectorTimeout bounds one collector, so one slow domain costs its own
	// section rather than the export.
	CollectorTimeout time.Duration `env:"COLLECTOR_TIMEOUT" json:"collectorTimeout,omitempty" yaml:"collectorTimeout,omitempty"`

	// ArtifactTTL is how long an export artifact survives after completion,
	// stamped onto the request as ExpiresAt when the Worker writes it. Defaults
	// to DefaultArtifactTTL.
	ArtifactTTL time.Duration `env:"ARTIFACT_TTL" json:"artifactTTL,omitempty" yaml:"artifactTTL,omitempty"`

	// MaxDocumentBytes caps the assembled export. Defaults to
	// DefaultMaxDocumentBytes.
	MaxDocumentBytes int64 `env:"MAX_DOCUMENT_BYTES" json:"maxDocumentBytes,omitempty" yaml:"maxDocumentBytes,omitempty"`

	// BatchSize is how many requests one cycle claims.
	BatchSize int `env:"BATCH_SIZE" json:"batchSize,omitempty" yaml:"batchSize,omitempty"`

	// Concurrency is how many claimed requests are fulfilled at once.
	Concurrency int `env:"CONCURRENCY" json:"concurrency,omitempty" yaml:"concurrency,omitempty"`

	// CollectorConcurrency is how many of one request's collectors run at once.
	//
	// It is bounded rather than unlimited because every collector queries the
	// application's own database, and a subject present in forty domains would
	// otherwise open forty concurrent queries on behalf of one background job.
	CollectorConcurrency int `env:"COLLECTOR_CONCURRENCY" json:"collectorConcurrency,omitempty" yaml:"collectorConcurrency,omitempty"`
}

var _ validation.ValidatableWithContext = (*WorkerConfig)(nil)

// EnsureDefaults fills unset knobs with the package defaults.
func (cfg *WorkerConfig) EnsureDefaults() {
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = DefaultPollInterval
	}
	if cfg.LeaseDuration <= 0 {
		cfg.LeaseDuration = DefaultLeaseDuration
	}
	if cfg.FulfillmentTimeout <= 0 {
		cfg.FulfillmentTimeout = DefaultFulfillmentTimeout
	}
	if cfg.CollectorTimeout <= 0 {
		cfg.CollectorTimeout = DefaultCollectorTimeout
	}
	if cfg.ArtifactTTL <= 0 {
		cfg.ArtifactTTL = DefaultArtifactTTL
	}
	if cfg.ArtifactPathPrefix == "" {
		cfg.ArtifactPathPrefix = DefaultArtifactPathPrefix
	}
	if cfg.MaxDocumentBytes <= 0 {
		cfg.MaxDocumentBytes = DefaultMaxDocumentBytes
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = DefaultBatchSize
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = DefaultConcurrency
	}
	if cfg.CollectorConcurrency <= 0 {
		cfg.CollectorConcurrency = DefaultCollectorConcurrency
	}

	cfg.Backoff.EnsureDefaults()
}

// ValidateWithContext validates a WorkerConfig.
func (cfg *WorkerConfig) ValidateWithContext(ctx context.Context) error {
	return validation.ValidateStructWithContext(ctx, cfg,
		validation.Field(&cfg.PollInterval, validation.Required),
		validation.Field(&cfg.BatchSize, validation.Required, validation.Min(1)),
		validation.Field(&cfg.Concurrency, validation.Required, validation.Min(1)),
		validation.Field(&cfg.CollectorConcurrency, validation.Required, validation.Min(1)),
		validation.Field(&cfg.CollectorTimeout, validation.Required),
		validation.Field(&cfg.FulfillmentTimeout, validation.Required),
		validation.Field(&cfg.ArtifactTTL, validation.Required),
		validation.Field(&cfg.ArtifactPathPrefix, validation.Required),
		validation.Field(&cfg.LeaseDuration, validation.Required, validation.By(func(any) error {
			// A lease that expires while the work it covers is still running is
			// not a lease. Erasure is the case that makes this load-bearing:
			// two workers running the same erasure concurrently means two sets
			// of deletes racing inside two transactions, and whichever loses
			// reports a failure for work that did happen.
			if cfg.LeaseDuration <= cfg.FulfillmentTimeout {
				return platformerrors.Newf(
					"dataprivacy lease duration %s must exceed fulfillment timeout %s",
					cfg.LeaseDuration, cfg.FulfillmentTimeout,
				)
			}

			return nil
		})),
	)
}

// SweeperConfig configures the expiry, lapse, and retention sweeps.
type SweeperConfig struct {
	// RequestRetention is how long a terminal request record is kept. Defaults
	// to DefaultRequestRetention.
	RequestRetention time.Duration `env:"REQUEST_RETENTION" json:"requestRetention,omitempty" yaml:"requestRetention,omitempty"`

	// BatchSize caps how much one sweep tick does, so a long-neglected table is
	// trimmed over several passes instead of one statement that holds locks for
	// minutes.
	BatchSize int `env:"BATCH_SIZE" json:"batchSize,omitempty" yaml:"batchSize,omitempty"`

	// DisableReap stops the sweeper deleting terminal request records.
	//
	// It exists because "how long do we keep the record that somebody asked" is
	// a jurisdiction's answer and not a library's, and an operator whose answer
	// is "forever, and we will argue about it later" should be able to say so
	// without setting a retention of a hundred years.
	DisableReap bool `env:"DISABLE_REAP" json:"disableReap,omitempty" yaml:"disableReap,omitempty"`
}

var _ validation.ValidatableWithContext = (*SweeperConfig)(nil)

// EnsureDefaults fills unset knobs with the package defaults.
func (cfg *SweeperConfig) EnsureDefaults() {
	if cfg.RequestRetention <= 0 {
		cfg.RequestRetention = DefaultRequestRetention
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = DefaultSweepBatchSize
	}
}

// ValidateWithContext validates a SweeperConfig.
func (cfg *SweeperConfig) ValidateWithContext(ctx context.Context) error {
	return validation.ValidateStructWithContext(ctx, cfg,
		validation.Field(&cfg.RequestRetention, validation.Required),
		validation.Field(&cfg.BatchSize, validation.Required, validation.Min(1)),
	)
}
