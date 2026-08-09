package sessions

import (
	"time"
)

// Expiry names which of a Policy's two deadlines a session has passed.
type Expiry uint8

const (
	// ExpiryNone means the session is still live.
	ExpiryNone Expiry = iota
	// ExpiryAbsolute means the session outlived its absolute timeout, measured
	// from when it was established. No activity extends this one.
	ExpiryAbsolute
	// ExpiryIdle means the session went unread for longer than the idle
	// timeout.
	ExpiryIdle
)

// String renders the reason as it appears on the sessions_expired counter.
func (e Expiry) String() string {
	switch e {
	case ExpiryAbsolute:
		return "absolute"
	case ExpiryIdle:
		return "idle"
	case ExpiryNone:
		return "none"
	default:
		return "unknown"
	}
}

// Err returns the sentinel for this reason, or nil for ExpiryNone.
func (e Expiry) Err() error {
	switch e {
	case ExpiryAbsolute:
		return ErrAbsoluteTimeout
	case ExpiryIdle:
		return ErrIdleTimeout
	case ExpiryNone:
		return nil
	default:
		return ErrExpired
	}
}

// Policy is the expiry rule a Store enforces, and the reason both backends
// cannot disagree about when a session ends: they never evaluate it, the Store
// does, once.
//
// Two timeouts, because they answer different questions. Idle asks how long a
// user may walk away and come back; Absolute asks how long a session may exist
// at all, which is the only bound on a cookie somebody stole. Either may be
// disabled by setting it non-positive, but not both — see ErrNoTimeout.
type Policy struct {
	// Absolute bounds a session's total lifetime from CreatedAt. Non-positive
	// disables it.
	Absolute time.Duration
	// Idle bounds how long a session may go unread. Non-positive disables it.
	Idle time.Duration
	// Touch is how much of the idle window must elapse before a read refreshes
	// the idle deadline.
	//
	// It exists because an idle timeout is otherwise a write on every read. At
	// a hundred requests a second against one session that is a hundred writes
	// a second to say the same thing; with a touch interval it is one write per
	// interval. What it costs is precision: a session's idle deadline can be up
	// to one interval stale, so a session expires up to Touch early rather than
	// late. Early is the safe direction for a security control, which is why
	// the trade is available at all.
	//
	// Zero refreshes on every read. It must be shorter than Idle, and is
	// irrelevant when Idle is disabled — there is no idle deadline to refresh,
	// so nothing is ever touched.
	Touch time.Duration

	// Grace is how long an expired record is kept before the backing store is
	// allowed to reclaim it.
	//
	// It exists because a backend's own expiry would otherwise decide when
	// sessions end, and it is the wrong thing to decide it. A record a cache
	// has already dropped cannot be told apart from one that never existed, so
	// a user who idled out and a client presenting a forged identifier would
	// get the same answer; worse, expiry would then be evaluated by the cache
	// server's clock rather than by the store's, which is neither the clock the
	// policy was written against nor one a test can move.
	//
	// So every write asks the backend to keep the record for its deadline plus
	// this, and the store refuses it the moment the deadline passes. The
	// backend's expiry becomes a garbage collector rather than a security
	// control. What it costs is retained bytes for expired sessions; what it
	// buys is a deterministic timeout and a returning user who can be told why
	// they were signed out.
	//
	// Non-positive lets the backend reclaim the record exactly at the deadline,
	// which is the cheaper setting and the one that gives up the distinction.
	Grace time.Duration
}

// Validate reports whether a Policy is one a store can enforce, rejecting the
// combinations that would leave it either unbounded or unable to keep a session
// alive.
func (p Policy) Validate() error {
	if p.Absolute <= 0 && p.Idle <= 0 {
		return ErrNoTimeout
	}
	if p.Touch < 0 {
		return ErrNegativeTouchInterval
	}
	if p.Idle > 0 && p.Touch >= p.Idle {
		return ErrTouchExceedsIdleTimeout
	}

	return nil
}

// Deadline is the instant a session stops being usable if nothing touches it
// again: the earlier of the two deadlines, or the only one that is enabled.
func (p Policy) Deadline(createdAt, lastSeenAt time.Time) time.Time {
	var deadline time.Time

	if p.Absolute > 0 {
		deadline = createdAt.Add(p.Absolute)
	}

	if p.Idle > 0 {
		if idle := lastSeenAt.Add(p.Idle); deadline.IsZero() || idle.Before(deadline) {
			return idle
		}
	}

	return deadline
}

// Expiry reports which deadline, if either, now has passed.
//
// Absolute is checked first so that a session past both is reported as the one
// nothing could have prevented. Telling a user they were signed out for
// inactivity when they were in fact signed out on schedule is a worse answer
// than the reverse.
func (p Policy) Expiry(createdAt, lastSeenAt, now time.Time) Expiry {
	if p.Absolute > 0 && !now.Before(createdAt.Add(p.Absolute)) {
		return ExpiryAbsolute
	}

	if p.Idle > 0 && !now.Before(lastSeenAt.Add(p.Idle)) {
		return ExpiryIdle
	}

	return ExpiryNone
}

// TTL is how much longer a record written now should remain retrievable: the
// idle window, clipped to whatever is left of the absolute one.
//
// It is what a Store hands a Backend, so the backing store's own expiry lands
// on the same instant Deadline reports. A non-positive result means the session
// is already over and must not be written at all — callers reach Expiry first,
// which is where that is decided.
func (p Policy) TTL(createdAt, now time.Time) time.Duration {
	remaining := time.Duration(0)

	if p.Absolute > 0 {
		remaining = createdAt.Add(p.Absolute).Sub(now)
		if remaining <= 0 {
			return 0
		}
	}

	if p.Idle > 0 && (remaining <= 0 || p.Idle < remaining) {
		remaining = p.Idle
	}

	return remaining
}

// RetentionTTL is how long a backend should keep a record written now: its
// remaining life, plus the grace that lets an expired session still be
// diagnosed rather than merely missed. See Policy.Grace.
//
// It is what a Store hands a Backend. TTL is the deadline the Store itself
// enforces, and the two are deliberately different numbers.
func (p Policy) RetentionTTL(createdAt, now time.Time) time.Duration {
	ttl := p.TTL(createdAt, now)
	if ttl <= 0 {
		return 0
	}

	if p.Grace > 0 {
		ttl += p.Grace
	}

	return ttl
}

// ShouldTouch reports whether a read should refresh the idle deadline.
//
// It is false whenever the idle timeout is disabled: with no idle deadline
// there is nothing for a touch to extend, and writing on every read to update a
// field nobody expires against is pure cost.
func (p Policy) ShouldTouch(lastSeenAt, now time.Time) bool {
	if p.Idle <= 0 {
		return false
	}

	return now.Sub(lastSeenAt) >= p.Touch
}
