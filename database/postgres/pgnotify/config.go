package pgnotify

import (
	"context"
	"time"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

const (
	// MaxChannelLength bounds a channel name. It is Postgres' NAMEDATALEN - 1:
	// the server truncates anything longer, and a truncated name is a name that
	// may no longer be the one the producer is notifying.
	MaxChannelLength = 63

	// DefaultMinReconnectBackoff is how long the listener waits before its first
	// reconnect attempt.
	DefaultMinReconnectBackoff = 100 * time.Millisecond

	// DefaultMaxReconnectBackoff caps the reconnect delay. It is deliberately
	// short for a backoff ceiling: while the listener is away the consumer is
	// running on its poll interval alone, so this bounds how long a cluster
	// failover degrades latency rather than how long a doomed connection is
	// retried.
	DefaultMaxReconnectBackoff = 30 * time.Second
)

// Config configures a Listener.
//
// There is no pool here, and no database.Client. LISTEN pins a session for the
// lifetime of the process, and postgres.PgxAccess's pools cap the union of the
// native and database/sql surfaces — so a listener that borrowed a connection
// would permanently cost the application one pool slot. It dials its own.
type Config struct {
	// ConnectionString is the DSN the listener dials. It must reach the primary:
	// NOTIFY does not cross replication, so a replica connects, listens, and
	// hears nothing forever.
	//
	// It must also not pass through a pooler in transaction or statement
	// pooling mode, which breaks LISTEN outright. See the package
	// documentation.
	ConnectionString string `env:"CONNECTION_STRING" json:"connectionString,omitempty" yaml:"connectionString,omitempty"`

	// Channel is the NOTIFY channel to listen on. It is validated as a SQL
	// identifier because LISTEN is a utility statement and cannot bind
	// parameters — the name is rendered into the statement, quoted, rather than
	// bound.
	//
	// It must match the name the producer notifies byte for byte; prefer
	// lowercase.
	Channel string `env:"CHANNEL" json:"channel,omitempty" yaml:"channel,omitempty"`

	// MinReconnectBackoff is the delay before the first reconnect attempt,
	// doubling up to MaxReconnectBackoff and resetting once a session is
	// established.
	MinReconnectBackoff time.Duration `env:"MIN_RECONNECT_BACKOFF" json:"minReconnectBackoff,omitempty" yaml:"minReconnectBackoff,omitempty"`

	// MaxReconnectBackoff caps the reconnect delay.
	MaxReconnectBackoff time.Duration `env:"MAX_RECONNECT_BACKOFF" json:"maxReconnectBackoff,omitempty" yaml:"maxReconnectBackoff,omitempty"`
}

var _ validation.ValidatableWithContext = (*Config)(nil)

// EnsureDefaults fills unset knobs with the package defaults.
func (cfg *Config) EnsureDefaults() {
	if cfg.MinReconnectBackoff <= 0 {
		cfg.MinReconnectBackoff = DefaultMinReconnectBackoff
	}
	if cfg.MaxReconnectBackoff <= 0 {
		cfg.MaxReconnectBackoff = DefaultMaxReconnectBackoff
	}
	if cfg.MaxReconnectBackoff < cfg.MinReconnectBackoff {
		cfg.MaxReconnectBackoff = cfg.MinReconnectBackoff
	}
}

// ValidateWithContext validates a Config.
func (cfg *Config) ValidateWithContext(ctx context.Context) error {
	return validation.ValidateStructWithContext(ctx, cfg,
		validation.Field(&cfg.ConnectionString, validation.Required),
		validation.Field(&cfg.Channel, validation.Required, validation.Length(1, MaxChannelLength)),
		validation.Field(&cfg.MinReconnectBackoff, validation.Required, validation.Min(time.Millisecond)),
		validation.Field(&cfg.MaxReconnectBackoff, validation.Required, validation.Min(cfg.MinReconnectBackoff)),
	)
}
