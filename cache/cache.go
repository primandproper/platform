package cache

import (
	"context"
	"time"

	"github.com/primandproper/platform-go/v7/errors"
)

var (
	ErrNotFound = errors.New("not found")

	// ErrNamespaceRequired indicates a Flush (or an unscoped DeleteByPrefix)
	// was attempted on a provider whose backing store may be shared and which
	// has no configured key namespace — so it cannot know which entries it
	// owns. Configure a namespace (e.g. the redis provider's
	// Config.Namespace) to enable whole-cache operations.
	ErrNamespaceRequired = errors.New("operation requires a configured key namespace")
)

// NoExpiry is the WithExpiry value for entries that should never expire. It
// is distinct from zero: a zero expiry means "use the cache's configured
// default".
const NoExpiry time.Duration = -1

type (
	// Cache is our wrapper interface for a cache. Batched reads, writes, and
	// deletes are part of the interface — every provider implements them, and
	// batching is the primary access pattern for high-volume consumers.
	//
	// Writes accept optional WriteOptions. A write with no options (or a zero
	// expiry) uses the cache's configured default expiry; WithExpiry overrides
	// it per call, and WithExpiry(NoExpiry) pins the entry against expiry
	// entirely.
	//
	// Keys are scoped to the cache instance: a provider with a configured
	// namespace prepends it to every key transparently, so callers never see
	// or supply namespaced keys. There is deliberately no way to reach
	// entries outside the cache's own namespace — a generic
	// inspect-everything client is a debugging tool, not a production
	// surface, and belongs in a separate Debug type if it is ever needed.
	Cache[T any] interface {
		Get(ctx context.Context, key string) (*T, error)
		// GetMany fetches multiple keys in as few round trips as possible.
		// Missing keys are omitted from the returned map, so a key's absence
		// from the result is a cache miss.
		GetMany(ctx context.Context, keys []string) (map[string]*T, error)
		Set(ctx context.Context, key string, value *T, opts ...WriteOption) error
		// SetMany stores multiple values at once. WriteOptions apply to the
		// whole batch: every item gets the same expiry resolution as a single
		// Set call would.
		SetMany(ctx context.Context, items map[string]*T, opts ...WriteOption) error
		Delete(ctx context.Context, key string) error
		// DeleteMany removes multiple keys in as few round trips as possible.
		// Keys that are already absent are not an error.
		DeleteMany(ctx context.Context, keys []string) error
		// DeleteByPrefix removes every entry whose key begins with prefix.
		// Providers on shared backing stores that have no configured
		// namespace reject an empty prefix with ErrNamespaceRequired, since
		// that would delete entries they cannot prove they own.
		DeleteByPrefix(ctx context.Context, prefix string) error
		// Flush removes every entry this cache owns. Providers whose backing
		// store may be shared (redis) require a configured namespace to know
		// what they own and return ErrNamespaceRequired without one;
		// providers that wholly own their store (memory) always succeed.
		Flush(ctx context.Context) error
		Ping(ctx context.Context) error
	}

	// WriteConfig is the resolved per-call write configuration. Providers
	// normally consume it through EffectiveExpiry rather than directly; it is
	// exported so third-party Cache implementations can run the same
	// resolution.
	WriteConfig struct {
		// Expiry holds the caller's requested expiry: zero for "use the
		// cache's default", NoExpiry (or any negative value) for "never
		// expire", or a positive duration.
		Expiry time.Duration
	}

	// WriteOption configures a single Set or SetMany call.
	WriteOption func(*WriteConfig)
)

// WithExpiry sets the expiry for the entries written by this call. Zero
// defers to the cache's configured default; NoExpiry (or any negative
// duration) stores the entries without expiry.
func WithExpiry(expiry time.Duration) WriteOption {
	return func(c *WriteConfig) {
		c.Expiry = expiry
	}
}

// EffectiveExpiry resolves a write's options against a cache's default
// expiry, returning the duration the entry should live: a positive duration,
// or zero meaning "never expire". The three input states resolve as
// documented on WithExpiry — an unset/zero expiry takes defaultExpiry, and a
// negative expiry (NoExpiry) or negative default resolves to no expiry.
// Providers should treat this as the single source of truth for expiry
// semantics so backends cannot drift.
func EffectiveExpiry(defaultExpiry time.Duration, opts ...WriteOption) time.Duration {
	var cfg WriteConfig
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	expiry := cfg.Expiry
	if expiry == 0 {
		expiry = defaultExpiry
	}
	if expiry < 0 {
		expiry = 0
	}

	return expiry
}
