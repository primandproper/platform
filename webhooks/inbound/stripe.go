package inbound

import (
	"context"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/primandproper/platform-go/v10/cryptography/hashing"
	"github.com/primandproper/platform-go/v10/cryptography/hashing/hmac"
	platformerrors "github.com/primandproper/platform-go/v10/errors"
)

const (
	// StripeSignatureHeader carries Stripe's timestamp and signatures.
	StripeSignatureHeader = "Stripe-Signature"

	// DefaultTolerance is how far a signed timestamp may sit from the
	// verifier's clock before a delivery is rejected as stale.
	//
	// Five minutes is Stripe's own default and the customary figure generally.
	// It is a compromise between two real failures: too tight and ordinary
	// clock skew rejects good deliveries, too loose and a captured delivery
	// stays replayable for as long as the window lasts.
	DefaultTolerance = 5 * time.Minute

	// stripeTimestampElement and stripeSignatureElement are the element keys in
	// the Stripe-Signature header, which is a comma-separated list of
	// key=value pairs: "t=1614556800,v1=abc…,v1=def…".
	stripeTimestampElement = "t"
	stripeSignatureElement = "v1"

	// providerStripe is the Provider label NewStripeVerifier reports.
	providerStripe = "stripe"
)

// stripeVerifier verifies Stripe's t=…,v1=… scheme.
type stripeVerifier struct {
	cfg     *verifierConfig
	hashers []hashing.Hasher
}

var _ Verifier = (*stripeVerifier)(nil)

// NewStripeVerifier builds a Verifier for Stripe's Stripe-Signature header.
//
// The scheme signs "<timestamp>.<body>" — the timestamp, a literal period, and
// the raw body — with HMAC-SHA-256 under the endpoint's signing secret, and
// sends the timestamp and the hex MAC as elements of one header. Signing the
// timestamp alongside the body is what makes the freshness check meaningful:
// the value compared against the clock is inside the signed material, so an
// attacker replaying a captured delivery cannot move it.
//
// secret is the endpoint's signing secret, the "whsec_…" value. Reads
// WithAdditionalSecrets, WithTolerance, WithClock, and WithVerificationTime.
//
// A header may carry several v1 elements and any one of them matching is
// enough. Stripe emits one per active endpoint secret during its own secret
// rollover, so rejecting on the first mismatch would fail every delivery for
// the length of a rotation the receiver has no say in.
func NewStripeVerifier(secret string, opts ...VerifierOption) (Verifier, error) {
	cfg := newVerifierConfig(opts)

	secrets := cfg.secretsWith(secret)
	if len(secrets) == 0 {
		return nil, ErrNoSecret
	}

	hashers := make([]hashing.Hasher, 0, len(secrets))
	for _, s := range secrets {
		hashers = append(hashers, hmac.NewHMACSHA256Hasher([]byte(s)))
	}

	return &stripeVerifier{cfg: cfg, hashers: hashers}, nil
}

// Provider returns "stripe".
func (v *stripeVerifier) Provider() string { return providerStripe }

// Verify checks the Stripe-Signature header against body.
//
// The staleness check runs before any HMAC work, so a flood of replayed
// deliveries costs a parse rather than a hash per key. It runs on the
// timestamp as presented, which is unauthenticated at that point — but a
// forged timestamp only ever moves a delivery out of the window or leaves it
// signed under a payload whose MAC will not match, so nothing is decided on an
// unverified value.
func (v *stripeVerifier) Verify(_ context.Context, headers http.Header, body []byte) error {
	// A nil header bag reads as an absent header rather than a special case, which is what
	// http.Header.Get already does.
	presented := headers.Get(StripeSignatureHeader)
	if presented == "" {
		return platformerrors.Wrapf(ErrInvalidSignature, "no %s header", StripeSignatureHeader)
	}

	sig := parseStripeSignature(presented)
	if sig.timestamp.IsZero() || len(sig.candidates) == 0 {
		return platformerrors.Wrapf(ErrInvalidSignature, "malformed %s header", StripeSignatureHeader)
	}

	if drift := v.cfg.now().Sub(sig.timestamp); drift > v.cfg.tolerance || drift < -v.cfg.tolerance {
		return platformerrors.Wrapf(ErrStaleSignature, "signed %s from now", drift.Round(time.Second))
	}

	// The signed payload carries the timestamp exactly as it appeared in the
	// header, not a re-rendering of the parsed time: a provider that ever pads
	// or formats it differently signed the bytes it sent, and re-rendering
	// would produce a MAC over bytes nobody signed.
	signed := make([]byte, 0, len(sig.rawTimestamp)+1+len(body))
	signed = append(signed, sig.rawTimestamp...)
	signed = append(signed, '.')
	signed = append(signed, body...)

	var matched bool
	for _, candidate := range sig.candidates {
		if matchesAny(v.hashers, signed, candidate) {
			matched = true
		}
	}

	if !matched {
		return ErrInvalidSignature
	}

	return nil
}

// stripeSignature is a parsed Stripe-Signature header.
type stripeSignature struct {
	// rawTimestamp is the t element's text, which is what gets signed.
	rawTimestamp string
	// timestamp is rawTimestamp parsed, which is what gets compared to a clock.
	timestamp time.Time
	// candidates are the decoded v1 elements, any one of which may match.
	candidates [][]byte
}

// parseStripeSignature pulls the timestamp and every v1 signature out of a
// Stripe-Signature header value.
//
// Unrecognized elements are skipped rather than rejected: Stripe has added
// elements to this header before (v0, for its test-mode scheme) and will
// again, and a verifier that failed on an element it did not know would be
// broken by a change that was designed to be backward compatible.
func parseStripeSignature(header string) stripeSignature {
	var sig stripeSignature

	for element := range strings.SplitSeq(header, ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(element), "=")
		if !ok {
			continue
		}

		switch key {
		case stripeTimestampElement:
			seconds, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				return stripeSignature{}
			}

			sig.rawTimestamp, sig.timestamp = value, time.Unix(seconds, 0)
		case stripeSignatureElement:
			decoded, err := hex.DecodeString(value)
			if err != nil {
				// One unreadable v1 among several is not fatal: the header is a
				// list precisely so that some of it may not apply to us.
				continue
			}

			sig.candidates = append(sig.candidates, decoded)
		}
	}

	return sig
}
