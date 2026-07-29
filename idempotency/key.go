package idempotency

import (
	"context"

	"github.com/primandproper/platform-go/v8/identifiers"
)

// Observability keys. Package-specific ones are namespaced; nothing here maps
// onto an observability/keys constant.
const (
	keyKey           = "idempotency.key"
	fingerprintKey   = "idempotency.fingerprint"
	claimIDKey       = "idempotency.claim_id"
	recordVersionKey = "idempotency.record_version"
	replayedKey      = "idempotency.replayed"
	recordedKey      = "idempotency.recorded"
	actionKey        = "idempotency.action"
	outcomeKey       = "outcome"
)

// Outcomes reported on idempotency_requests. Every call to Do that resolves
// lands in exactly one of these, so the four together are the request total.
const (
	outcomeExecuted = "executed"
	outcomeReplayed = "replayed"
	outcomeInFlight = "in_flight"
	outcomeMismatch = "mismatch"
)

// ValidateKey reports whether a client-supplied key is usable.
//
// A key becomes both a store key and a lock key, so it is restricted rather
// than escaped: printable ASCII with no spaces, which admits the UUIDs, xids,
// and base64url tokens clients actually send while excluding control
// characters and anything that would travel badly in a header.
//
// identifiers.Validate is deliberately not used. It accepts only xid, and the
// keys arriving here are minted by third-party clients — rejecting a
// well-formed UUID would break every caller that does the ordinary thing.
// Generating a key is the other direction; see WithNewKey.
//
// A non-positive maxLength disables the length check.
func ValidateKey(key string, maxLength int) error {
	if key == "" {
		return ErrKeyRequired
	}

	if maxLength > 0 && len(key) > maxLength {
		return ErrKeyTooLong
	}

	for i := range len(key) {
		// Bytes, not runes: the check is over the wire representation, and
		// anything outside this range is rejected whole rather than decoded.
		if c := key[i]; c <= ' ' || c > '~' {
			return ErrKeyInvalid
		}
	}

	return nil
}

// keyContextKey types the context value, so nothing else can collide with it.
type keyContextKey struct{}

// WithKey returns a context carrying key, for a client adapter to attach to
// outbound requests.
func WithKey(ctx context.Context, key string) context.Context {
	return context.WithValue(ctx, keyContextKey{}, key)
}

// WithNewKey returns a context carrying a freshly minted key, and the key.
//
// Call it once per logical operation, outside any retry loop. That placement
// is the whole contract: every attempt sharing this context sends the same
// key, which is what lets the server recognize a retry. Minting inside the
// loop produces a new key per attempt and no protection at all.
//
// The generator is identifiers.New, which is fine for keys this process mints
// even though inbound keys are validated by shape rather than by xid — see
// ValidateKey.
func WithNewKey(ctx context.Context) (keyed context.Context, key string) {
	key = identifiers.New()

	return WithKey(ctx, key), key
}

// KeyFromContext returns the key carried by ctx, if any.
func KeyFromContext(ctx context.Context) (string, bool) {
	key, ok := ctx.Value(keyContextKey{}).(string)
	if !ok || key == "" {
		return "", false
	}

	return key, true
}
