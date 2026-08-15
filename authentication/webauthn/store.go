package webauthn

import (
	"context"
	"time"
)

// SessionStore holds a ceremony's state between the request that issued the
// challenge and the request that answers it.
//
// Two methods, and the second is the interesting one. Consume fetches and
// removes in a single operation, so a challenge is answerable exactly once: an
// assertion replayed inside its TTL finds nothing the second time. A Get and a
// Delete would leave that guarantee to whoever remembered to call the Delete,
// on the success path, after the validation that might have returned early.
//
// There is deliberately no non-consuming read. Nothing in a ceremony needs one
// — the Begin issues, the Finish answers — and offering one would offer the
// replay this interface exists to prevent.
//
// # What an implementation owes
//
// Save stores session under its own Challenge for ttl. It reports
// ErrNilSession for a nil session, ErrChallengeRequired for one whose Challenge
// is empty, and ErrNonPositiveTTL for a ttl of zero or less. The challenge is
// taken from the session rather than passed beside it, so a session cannot be
// stored under a key that is not the one a Finish will look it up by.
//
// Consume returns the state stored under challenge and removes it. It reports
// ErrSessionNotFound when the challenge was never stored, has already been
// consumed, or has passed its TTL — an implementation that can tell the last
// case apart may report ErrSessionExpired, which wraps ErrSessionNotFound.
// Where the backing store can do it, exactly one of several concurrent
// consumers of one challenge gets the state and the rest are told
// ErrSessionNotFound; where it cannot, the implementation's doc says so and it
// declares the deviation to the conformance suite in
// authentication/webauthn/webauthntest.
//
// A round trip preserves what the ceremony needs: the challenge, the user
// handle, the allowed credential IDs, the user verification requirement, and
// the deadline. Timestamps come back as UTC and may be truncated to
// microseconds, which is what the supported column types store.
//
// # What it does not own
//
// Registered credentials. A [Credential] is the application's to store, for as
// long as the passkey exists; this interface holds the seconds-long state of
// one ceremony and nothing else.
type SessionStore interface {
	Save(ctx context.Context, session *SessionData, ttl time.Duration) error
	Consume(ctx context.Context, challenge string) (*SessionData, error)
}

// ValidateSession applies the argument rules every SessionStore.Save owes its
// callers, so that the two implementations here — and any third one — reject
// the same inputs with the same sentinels instead of each deciding.
//
// It is exported for that third implementation's benefit. The rules are
// otherwise a comment on an interface, which is the kind of contract that
// holds until somebody writes a store that stores a nil session under an empty
// key for a TTL of zero and finds that nothing complained.
func ValidateSession(session *SessionData, ttl time.Duration) error {
	if session == nil {
		return ErrNilSession
	}

	if session.Challenge == "" {
		return ErrChallengeRequired
	}

	if ttl <= 0 {
		return ErrNonPositiveTTL
	}

	return nil
}
