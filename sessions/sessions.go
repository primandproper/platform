package sessions

import (
	"context"
	"time"
)

const (
	// DefaultAbsoluteTimeout is how long a session may live from the moment it
	// was established, regardless of activity. It is the ceiling a stolen
	// cookie cannot outlive, so it is the one timeout that has to be set even
	// on a store nobody idles out.
	DefaultAbsoluteTimeout = 24 * time.Hour

	// DefaultIdleTimeout is how long a session survives without being read.
	DefaultIdleTimeout = 30 * time.Minute

	// DefaultTouchInterval is how much of the idle window has to elapse before
	// a read refreshes the session's idle deadline. See Policy for why this is
	// not zero.
	DefaultTouchInterval = time.Minute

	// DefaultRetentionGrace is how long an expired record is kept before the
	// backing store may reclaim it, so that a user who comes back can be told
	// why they were signed out rather than merely that they were. See
	// Policy.Grace for why a backend's own expiry is the wrong thing to end a
	// session with.
	DefaultRetentionGrace = time.Hour

	// DefaultIDByteLength is how many random bytes a session identifier is
	// minted from — 256 bits, which is what makes the cache backend's
	// collision-free Create safe to assume.
	DefaultIDByteLength = 32

	// serviceName names the loggers, spans, and metrics this package emits.
	serviceName = "sessions"

	// recordVersion stamps every record written. A deploy that changes the
	// shape of T (or of Record itself) bumps it, and records written by the
	// previous shape read as absent rather than being misread — a stale
	// session is a re-login, where a misread one is a user holding somebody
	// else's payload.
	recordVersion = 1

	// timeResolution is what every stamped time is truncated to.
	//
	// It is here rather than in the database backend because both backends
	// have to agree: Postgres and MySQL keep microseconds, so a nanosecond
	// stamp comes back changed, and a Session read from the database would
	// then differ from the one New just returned for the same session. The
	// cache backend round-trips whatever it is given, so truncating at the
	// stamping site is what makes the two interchangeable.
	timeResolution = time.Microsecond
)

type (
	// Record is what a Backend holds for a session identifier. It carries the
	// payload and the two anchors expiry is measured from, and nothing else —
	// the identifier is the key it is stored under, and the deadlines are
	// derived from the Policy rather than frozen into the record.
	//
	// T must round-trip through whichever backend stores it. The cache backend
	// serializes with its provider's codec (CBOR by default), the database
	// backend with an encoding.Codec; both want a concrete struct with
	// exported fields.
	Record[T any] struct {
		// CreatedAt is when the session was established. It survives Renew, so
		// rotating an identifier does not extend the absolute deadline — which
		// is the whole reason rotation is safe to do on every privilege change.
		CreatedAt time.Time
		// LastSeenAt is when the session was last read or written. It is the
		// idle deadline's anchor, and it is refreshed no more often than the
		// Policy's touch interval — see Policy.
		LastSeenAt time.Time
		// Data is the payload. It may be nil: a session that only needs to
		// exist is a legitimate session.
		Data *T
		// Version is the record shape this was written with.
		Version int
	}

	// Session is a live session as a Store hands it back.
	//
	// It is a snapshot, not a handle: mutating it changes nothing server-side.
	// Store.Save is how a payload is written back.
	Session[T any] struct {
		// CreatedAt is when the session was established, unchanged by Renew.
		CreatedAt time.Time
		// LastSeenAt is the idle deadline's anchor as of this read.
		LastSeenAt time.Time
		// ExpiresAt is the earlier of the absolute and idle deadlines: the
		// instant this session stops being usable if nothing touches it again.
		// It is what a cookie's MaxAge should be derived from, so the browser
		// and the store agree on when the session ended.
		ExpiresAt time.Time
		// Data is the payload, as stored.
		Data *T
		// ID is the identifier this session was read under. It is the value the
		// cookie carries, and the only part of a session that ever leaves the
		// server.
		ID string
	}

	// Store is the server-side session store: identifiers in, payloads out.
	//
	// Every method takes or returns an identifier rather than a cookie. What
	// the identifier travels in is the caller's business — sessions/http binds
	// it to a signed cookie, which is what nearly everyone wants.
	//
	// A Store enforces the expiry Policy and mints identifiers; where the
	// records physically live is the Backend's business. Absence and expiry are
	// reported as ErrNotFound and ErrExpired, and ErrExpired wraps ErrNotFound,
	// so a caller that does not care about the difference checks one thing.
	Store[T any] interface {
		// New establishes a session around data and returns it, identifier
		// included. data may be nil.
		New(ctx context.Context, data *T) (*Session[T], error)
		// Get reads a session, refreshing its idle deadline when the Policy's
		// touch interval has elapsed.
		//
		// A session past either deadline is reported as ErrExpired and removed;
		// one that was never there, or whose record was written by another
		// shape of this package, is reported as ErrNotFound.
		Get(ctx context.Context, id string) (*Session[T], error)
		// Save replaces a session's payload. It refreshes the idle deadline and
		// leaves the absolute one alone.
		Save(ctx context.Context, id string, data *T) error
		// Renew rotates a session's identifier, carrying the payload and the
		// original CreatedAt across, and returns the new identifier.
		//
		// Call it on every privilege change — sign-in above all. Without it, an
		// identifier an attacker planted before sign-in is still valid after
		// it, which is session fixation. Because CreatedAt survives, rotating
		// on every privilege change cannot be used to extend a session forever.
		//
		// The old identifier stops working the moment this returns nil. If it
		// returns an error, assume it still works and refuse the privilege
		// change.
		Renew(ctx context.Context, oldID string) (newID string, err error)
		// Delete ends a session. An identifier that was already gone is not an
		// error: sign-out is not the place to surface a race.
		Delete(ctx context.Context, id string) error
		// Policy reports the expiry rule this store enforces.
		//
		// It is on the interface because the cookie has to agree with it:
		// sessions/http derives how long a browser should keep a session
		// cookie from the absolute timeout rather than from a second setting
		// that could drift from this one.
		Policy() Policy
		// Close releases what the store holds — the backend's connection pool,
		// a background sweep — and is safe to call more than once.
		Close() error
	}

	// Backend is where a Store's records physically live. sessions/cache and
	// sessions/database implement it; a Store adds identifiers, expiry, and
	// observability on top.
	//
	// The split exists because the parts worth getting exactly right — what
	// Renew preserves, when a session is expired, whether a touch may
	// resurrect a signed-out session — are the parts that must not differ
	// between backends. Written once in Store, they cannot.
	//
	// Every method's ttl is how long the record should remain retrievable, and
	// is always positive: a Store never asks a backend to store something
	// already expired.
	Backend[T any] interface {
		// Load reads the record stored under id, reporting ErrNotFound when
		// there is none. It does not evaluate expiry — the Store does, from the
		// record's own anchors, so both backends answer the same question the
		// same way.
		Load(ctx context.Context, id string) (*Record[T], error)
		// Create stores a record under an identifier that must not already
		// exist, reporting ErrIDConflict if it does.
		Create(ctx context.Context, id string, record *Record[T], ttl time.Duration) error
		// Update overwrites the record stored under an existing identifier,
		// reporting ErrNotFound when there is none.
		//
		// The existence requirement is not bookkeeping. Without it a request
		// that read a session just before it was signed out would write it back
		// afterwards, and the sign-out would not have happened.
		Update(ctx context.Context, id string, record *Record[T], ttl time.Duration) error
		// Rename moves a record from oldID to newID, reporting ErrNotFound when
		// oldID holds nothing. On a nil return, oldID no longer resolves.
		Rename(ctx context.Context, oldID, newID string, record *Record[T], ttl time.Duration) error
		// Delete removes the record stored under id. An identifier that was
		// already absent is not an error.
		Delete(ctx context.Context, id string) error
		// Close releases the backend's resources and is safe to call more than
		// once.
		Close() error
	}
)

// session renders a stored record as the snapshot callers see.
func (s *store[T]) session(id string, record *Record[T]) *Session[T] {
	return &Session[T]{
		CreatedAt:  record.CreatedAt,
		LastSeenAt: record.LastSeenAt,
		ExpiresAt:  s.policy.Deadline(record.CreatedAt, record.LastSeenAt),
		Data:       record.Data,
		ID:         id,
	}
}
