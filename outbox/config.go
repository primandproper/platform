package outbox

import (
	"context"
	"time"

	"github.com/primandproper/platform-go/v9/database/dialect"
	platformerrors "github.com/primandproper/platform-go/v9/errors"
	retrycfg "github.com/primandproper/platform-go/v9/retry/config"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

const (
	// DefaultBatchSize is how many messages one cycle claims.
	DefaultBatchSize = 100
	// DefaultPollInterval is how often the relay looks for work.
	DefaultPollInterval = time.Second
	// DefaultLeaseDuration is how long a claim is held before another relay may
	// reclaim the message. It must comfortably exceed the time to publish a
	// batch, or two relays will publish the same message concurrently.
	DefaultLeaseDuration = 30 * time.Second
	// DefaultRetention is how long published rows are kept before reaping.
	DefaultRetention = 24 * time.Hour
	// DefaultReapInterval is how often the reaper runs.
	DefaultReapInterval = 5 * time.Minute
	// DefaultReapBatchSize caps one reap, so a large backlog is removed over
	// several passes instead of one long-running DELETE.
	DefaultReapBatchSize = 1000
)

// ClaimMode selects how the relay takes ownership of messages.
type ClaimMode string

const (
	// ClaimSkipLocked claims with FOR UPDATE SKIP LOCKED, so several relays can
	// run at once without contending. Requires Postgres or MySQL.
	ClaimSkipLocked ClaimMode = "skip_locked"
	// ClaimLease claims with a lease alone. Correct everywhere — and the only
	// option on SQLite — and the right choice when a single relay is running.
	ClaimLease ClaimMode = "lease"
)

// Valid reports whether m is a known claim mode.
func (m ClaimMode) Valid() bool {
	return m == ClaimSkipLocked || m == ClaimLease
}

// RelayConfig configures a Relay.
//
// There is deliberately no Dialect field. The SQL a relay emits has to match
// the database it runs against, and a config that names the dialect separately
// makes the mismatch representable — so NewRelay reads it from the
// database.Client instead, which is the one thing that cannot be wrong about
// its own dialect.
type RelayConfig struct {
	// TablePrefix is the namespace the outbox table carries. Empty renders
	// outbox_messages; "ddb" renders ddb_outbox_messages.
	TablePrefix string `env:"TABLE_PREFIX" json:"tablePrefix" yaml:"tablePrefix"`
	// table is TablePrefix resolved to a full name, filled by EnsureDefaults so
	// every query builder below reads one already-qualified string.
	table string
	// ClaimMode selects lease-only or SKIP LOCKED claiming.
	ClaimMode ClaimMode `env:"CLAIM_MODE" json:"claimMode" yaml:"claimMode"`
	// Backoff drives the retry schedule for messages that fail to publish.
	// MaxAttempts is the quarantine threshold.
	Backoff retrycfg.Config `envPrefix:"BACKOFF_" json:"backoff" yaml:"backoff"`
	// BatchSize is how many messages one cycle claims.
	BatchSize int `env:"BATCH_SIZE" json:"batchSize" yaml:"batchSize"`
	// PollInterval is how often the relay looks for work.
	PollInterval time.Duration `env:"POLL_INTERVAL" json:"pollInterval" yaml:"pollInterval"`
	// LeaseDuration is how long a claim is held before it can be reclaimed.
	LeaseDuration time.Duration `env:"LEASE_DURATION" json:"leaseDuration" yaml:"leaseDuration"`
	// Retention is how long published rows are kept before reaping.
	Retention time.Duration `env:"RETENTION" json:"retention" yaml:"retention"`
	// ReapInterval is how often the reaper runs.
	ReapInterval time.Duration `env:"REAP_INTERVAL" json:"reapInterval" yaml:"reapInterval"`
	// ReapBatchSize caps how many rows one reap deletes.
	ReapBatchSize int `env:"REAP_BATCH_SIZE" json:"reapBatchSize" yaml:"reapBatchSize"`
}

var _ validation.ValidatableWithContext = (*RelayConfig)(nil)

// EnsureDefaults fills unset knobs with the package defaults.
//
// The claim mode defaults to SKIP LOCKED here and is narrowed later by
// resolveForDialect, once NewRelay has read the dialect off the client: a
// dialect without SKIP LOCKED is forced to ClaimLease.
func (cfg *RelayConfig) EnsureDefaults() {
	cfg.table = tableFor(cfg.TablePrefix)
	if cfg.ClaimMode == "" {
		cfg.ClaimMode = ClaimSkipLocked
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = DefaultBatchSize
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = DefaultPollInterval
	}
	if cfg.LeaseDuration <= 0 {
		cfg.LeaseDuration = DefaultLeaseDuration
	}
	if cfg.Retention <= 0 {
		cfg.Retention = DefaultRetention
	}
	if cfg.ReapInterval <= 0 {
		cfg.ReapInterval = DefaultReapInterval
	}
	if cfg.ReapBatchSize <= 0 {
		cfg.ReapBatchSize = DefaultReapBatchSize
	}

	cfg.Backoff.EnsureDefaults()
}

// ValidateWithContext validates a RelayConfig.
func (cfg *RelayConfig) ValidateWithContext(ctx context.Context) error {
	return validation.ValidateStructWithContext(ctx, cfg,
		validation.Field(&cfg.ClaimMode, validation.Required, validation.By(func(any) error {
			if !cfg.ClaimMode.Valid() {
				return platformerrors.Wrapf(ErrInvalidClaimMode, "claim mode %q", cfg.ClaimMode)
			}

			return nil
		})),
		validation.Field(&cfg.BatchSize, validation.Required, validation.Min(1)),
		validation.Field(&cfg.PollInterval, validation.Required, validation.Min(time.Millisecond)),
		validation.Field(&cfg.LeaseDuration, validation.Required, validation.Min(time.Second)),
		validation.Field(&cfg.Retention, validation.Required, validation.Min(time.Minute)),
		validation.Field(&cfg.ReapInterval, validation.Required, validation.Min(time.Second)),
		validation.Field(&cfg.ReapBatchSize, validation.Required, validation.Min(1)),
	)
}

// resolveForDialect narrows the claim mode to what d can actually do, and
// reports a dialect this package cannot emit SQL for.
//
// It is separate from EnsureDefaults because the dialect is not the config's to
// know: NewRelay reads it from the database.Client and applies it here.
func (cfg *RelayConfig) resolveForDialect(d dialect.Dialect) error {
	if !d.Valid() {
		return platformerrors.Wrapf(dialect.ErrUnsupported, "outbox dialect %q", d)
	}

	// Only the SKIP LOCKED mode is downgraded. Rewriting any other value would
	// also rewrite a typo, hiding it from the validation that runs next.
	if cfg.ClaimMode == ClaimSkipLocked && !d.SupportsSkipLocked() {
		cfg.ClaimMode = ClaimLease
	}

	return nil
}
