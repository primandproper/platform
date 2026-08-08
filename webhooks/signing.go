package webhooks

import (
	"encoding/hex"
	"strconv"
	"strings"
	"time"

	"github.com/primandproper/platform-go/v10/cryptography/hashing"
	hmachasher "github.com/primandproper/platform-go/v10/cryptography/hashing/hmac"
	platformerrors "github.com/primandproper/platform-go/v10/errors"
)

const (
	// SignatureHeader carries the signature(s) over the request body.
	SignatureHeader = "X-Platform-Signature"
	// TimestampHeader carries the signing timestamp, as Unix seconds. It is the
	// same value that appears inside the signature; it is exposed separately so
	// a subscriber can reject a stale request before doing any HMAC work.
	TimestampHeader = "X-Platform-Timestamp"
	// EventTypeHeader carries the delivery's event type, so a subscriber can
	// route without parsing the body.
	EventTypeHeader = "X-Platform-Event"
	// DeliveryIDHeader carries the delivery ID, which is stable across retries
	// of the same delivery and is therefore the subscriber's deduplication key.
	DeliveryIDHeader = "X-Platform-Delivery"
	// AttemptHeader carries which attempt this is, 1-indexed, so a subscriber
	// can tell a first delivery from a redelivery.
	AttemptHeader = "X-Platform-Attempt"

	// SignatureSchemeV1 is the only scheme this package mints.
	SignatureSchemeV1 = "v1"

	// DefaultTolerance is how far a signature's timestamp may sit from the
	// verifier's clock before Verify rejects it.
	//
	// Five minutes is the customary figure, and it is a compromise between two
	// real failures: too tight and ordinary clock skew between sender and
	// subscriber rejects good deliveries, too loose and a captured request stays
	// replayable for as long as the window lasts.
	DefaultTolerance = 5 * time.Minute
)

var (
	// ErrInvalidSignature indicates a signature header that is malformed,
	// carries no recognized scheme, or does not match the body under the given
	// secret. The cases are deliberately one error: telling a caller which of
	// them applied tells an attacker how close a forgery came.
	ErrInvalidSignature = platformerrors.New("invalid webhook signature")

	// ErrStaleSignature indicates a signature whose timestamp is outside the
	// tolerance. It is distinct from ErrInvalidSignature because it is the one
	// verification failure with a benign cause an operator can act on — clock
	// skew — and it says nothing about the secret.
	ErrStaleSignature = platformerrors.New("webhook signature timestamp outside tolerance")
)

// signingPayload renders the bytes a signature actually covers:
//
//	<scheme>.<unix timestamp>.<body>
//
// The scheme and the timestamp are inside the signed material rather than
// merely alongside it, and both are load-bearing.
//
// The timestamp is what makes a captured request expire. Signing the body alone
// — which is what this package was extracted to replace — produces a signature
// valid forever, so anyone who observes one request can replay it against the
// subscriber indefinitely.
//
// The scheme prefix is what makes the construction replaceable. Without it, a
// v2 that signed different material could be substituted by an attacker for a
// v1 signature over material that happens to coincide, and a subscriber
// accepting both schemes during a migration would have no way to tell them
// apart. Binding the scheme into the signed bytes means a v1 signature can only
// ever verify as v1.
func signingPayload(scheme string, timestamp int64, body []byte) []byte {
	prefix := scheme + "." + strconv.FormatInt(timestamp, 10) + "."

	// Built in one allocation: this runs once per secret per attempt, on bodies
	// that can be large.
	buf := make([]byte, 0, len(prefix)+len(body))
	buf = append(buf, prefix...)
	buf = append(buf, body...)

	return buf
}

// Sign renders the SignatureHeader value for body at the given time, under
// every active key in secret.
//
// The result looks like:
//
//	v1,t=1753900000,s=<hex>,s=<hex>
//
// A second s= appears only during a rotation window, when secret.Previous is
// set. Emitting both is what lets a subscriber roll its key without
// coordinating an instant of downtime with whoever operates this sender: it
// accepts either signature while it switches, and the operator drops Previous
// once every subscriber has.
//
// Verify accepts a header with any number of s= components, so widening this to
// a longer key list later is not a wire change.
func Sign(secret Secret, body []byte, at time.Time) (string, error) {
	if len(secret.Current) == 0 {
		return "", ErrNoSigningSecret
	}

	timestamp := at.UTC().Unix()
	payload := signingPayload(SignatureSchemeV1, timestamp, body)

	var sb strings.Builder

	sb.WriteString(SignatureSchemeV1)
	sb.WriteString(",t=")
	sb.WriteString(strconv.FormatInt(timestamp, 10))

	for _, key := range [][]byte{secret.Current, secret.Previous} {
		if len(key) == 0 {
			continue
		}

		sb.WriteString(",s=")
		sb.WriteString(hashing.Hex(hmachasher.NewHMACSHA256Hasher(key), payload))
	}

	return sb.String(), nil
}

type verifyConfig struct {
	now       time.Time
	tolerance time.Duration
}

// Verify checks a SignatureHeader value against body under secret, and is what
// a subscriber calls on receipt.
//
// It ships with this package on purpose. Verification is where webhook schemes
// are actually got wrong: subscribers compare with ==, forget the timestamp
// check, or verify a re-serialized body rather than the received bytes. Handing
// out the sender and leaving the receiver to reimplement it from prose is how
// that keeps happening.
//
// body must be the exact bytes received, read before any JSON decoding.
// Decoding and re-encoding changes key order and whitespace, and the signature
// covers bytes, not meaning.
//
// A signature verifies if it matches under any key in secret, so a subscriber
// holding both an old and a new key accepts deliveries from either side of a
// rotation.
func Verify(secret Secret, body []byte, signature string, opts ...VerifyOption) error {
	cfg := &verifyConfig{tolerance: DefaultTolerance}
	for _, opt := range opts {
		if opt != nil {
			opt(cfg)
		}
	}

	if cfg.now.IsZero() {
		cfg.now = time.Now()
	}

	scheme, timestamp, candidates, err := parseSignature(signature)
	if err != nil {
		return err
	}

	// The timestamp is checked before any HMAC is computed. A stale request is
	// rejected without spending work proportional to its body, which is what
	// keeps a replay flood from costing the subscriber anything.
	if skew := cfg.now.UTC().Sub(time.Unix(timestamp, 0).UTC()); skew > cfg.tolerance || skew < -cfg.tolerance {
		return platformerrors.Wrapf(ErrStaleSignature, "timestamp %d is %s from now", timestamp, skew.Round(time.Second))
	}

	payload := signingPayload(scheme, timestamp, body)

	// Every key is tried, and the loop does not break on a match: returning as
	// soon as one key verifies makes the time taken depend on which key matched,
	// which distinguishes "current key" from "previous key" to anyone timing it.
	matched := false

	for _, key := range [][]byte{secret.Current, secret.Previous} {
		if len(key) == 0 {
			continue
		}

		expected := hmachasher.NewHMACSHA256Hasher(key).Hash(payload)

		for _, candidate := range candidates {
			if hmachasher.Equal(expected, candidate) {
				matched = true
			}
		}
	}

	if !matched {
		return ErrInvalidSignature
	}

	return nil
}

// maxSignatureComponents bounds how many components parseSignature will read,
// so an attacker cannot make a subscriber allocate — or HMAC-compare against —
// an unbounded list by sending a header full of s= parts.
const maxSignatureComponents = 16

// parseSignature splits a SignatureHeader value into its scheme, timestamp, and
// candidate MACs.
//
// Unknown components are ignored rather than rejected, so a later scheme may add
// one without every existing subscriber failing closed on it. An unknown
// *scheme* is rejected outright: that is a change to what the signature covers,
// and ignoring it would mean verifying v2 material under v1 rules.
func parseSignature(signature string) (scheme string, timestamp int64, candidates [][]byte, err error) {
	parts := strings.Split(signature, ",")
	if len(parts) < 2 || len(parts) > maxSignatureComponents {
		return "", 0, nil, ErrInvalidSignature
	}

	scheme = strings.TrimSpace(parts[0])
	if scheme != SignatureSchemeV1 {
		return "", 0, nil, platformerrors.Wrapf(ErrInvalidSignature, "unsupported signature scheme %q", scheme)
	}

	haveTimestamp := false

	for _, part := range parts[1:] {
		key, value, found := strings.Cut(strings.TrimSpace(part), "=")
		if !found {
			return "", 0, nil, ErrInvalidSignature
		}

		switch key {
		case "t":
			if haveTimestamp {
				// Two timestamps means two readings of what was signed, and
				// picking either one is a guess.
				return "", 0, nil, ErrInvalidSignature
			}

			if timestamp, err = strconv.ParseInt(value, 10, 64); err != nil {
				return "", 0, nil, ErrInvalidSignature
			}

			haveTimestamp = true
		case "s":
			mac, decodeErr := hex.DecodeString(value)
			if decodeErr != nil {
				return "", 0, nil, ErrInvalidSignature
			}

			candidates = append(candidates, mac)
		}
	}

	if !haveTimestamp || len(candidates) == 0 {
		return "", 0, nil, ErrInvalidSignature
	}

	return scheme, timestamp, candidates, nil
}
