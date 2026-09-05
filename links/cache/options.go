package cache

import (
	platformerrors "github.com/primandproper/platform-go/v14/errors"
	"github.com/primandproper/platform-go/v14/observability/logging"
	"github.com/primandproper/platform-go/v14/observability/tracing"
)

// DefaultKeyPrefix namespaces the record and lock keys, so a link cannot
// collide with an unrelated entry in a cache or a locker shared with something
// else.
const DefaultKeyPrefix = "links:"

var (
	// ErrNilCache indicates New was called without a cache. It wraps
	// errors.ErrNilInputParameter, so a caller may check either.
	ErrNilCache = platformerrors.Wrap(platformerrors.ErrNilInputParameter, "nil action link cache")

	// ErrNilLocker indicates New was called without a locker.
	//
	// It has no default, and the requirement lives here rather than on
	// links.NewMinter because it belongs to this store rather than to the
	// package: a cache cannot make a read and a write one operation, so without
	// mutual exclusion two requests carrying one token both read "active" and
	// both proceed. That is the entire failure links exists to prevent, and it
	// arrives silently and only under concurrency — the noop locker acquires
	// unconditionally, so every sequential test still passes.
	//
	// links/database needs none: a guarded UPDATE inside a transaction is
	// already the same promise, decided by the server.
	//
	// It wraps errors.ErrNilInputParameter, so a caller may check either.
	ErrNilLocker = platformerrors.Wrap(platformerrors.ErrNilInputParameter, "nil action link locker")
)

type (
	// Option configures the store at construction.
	//
	// There is no WithMetricsProvider. Every counter worth having describes
	// what an operation meant — a link minted, a redemption refused because it
	// was already spent — and only the Minter knows that; this layer would be
	// able to count round trips the cache provider already counts.
	Option func(*options)

	options struct {
		logger         logging.Logger
		tracerProvider tracing.Provider

		keyPrefix *string
	}
)

// newOptions applies opts, ignoring nil entries.
func newOptions(opts []Option) *options {
	o := &options{}
	for _, opt := range opts {
		if opt != nil {
			opt(o)
		}
	}

	return o
}

// WithKeyPrefix overrides the namespace applied to record and lock keys.
//
// An empty prefix is honored rather than ignored, so a caller can deliberately
// opt out of namespacing; that is why this is the one setting held as a
// pointer.
func WithKeyPrefix(prefix string) Option {
	return func(o *options) { o.keyPrefix = &prefix }
}

// WithLogger attaches a logger. An absent logger logs nowhere.
func WithLogger(logger logging.Logger) Option {
	return func(o *options) { o.logger = logger }
}

// WithTracerProvider attaches a tracer provider. An absent one traces nowhere.
func WithTracerProvider(tracerProvider tracing.Provider) Option {
	return func(o *options) { o.tracerProvider = tracerProvider }
}
