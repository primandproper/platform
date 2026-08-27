package inbound

import (
	"context"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/primandproper/platform-go/v13/cryptography/hashing"
	"github.com/primandproper/platform-go/v13/cryptography/hashing/hmac"
	platformerrors "github.com/primandproper/platform-go/v13/errors"
)

const (
	// StripeSignatureHeader carries Stripe's timestamp and signatures.
	StripeSignatureHeader = "Stripe-Signature"

	// RevenueCatSignatureHeader carries RevenueCat's timestamp and signature.
	//
	// RevenueCat also offers a dashboard-configured Authorization header, which
	// is a bearer token rather than a signature: it proves the sender knew a
	// secret, and says nothing about the body it was attached to. That mode is
	// deliberately not implemented here — a verifier for it would satisfy the
	// same interface while checking something weaker, and a receiver mounting
	// one could not tell from the type that its payloads were unauthenticated.
	// Turn signing on in the same dashboard page the header is configured on.
	RevenueCatSignatureHeader = "X-RevenueCat-Webhook-Signature"

	// timestampElement and signatureElement are the element keys in a
	// timestamped-HMAC header, which is a comma-separated list of key=value
	// pairs: "t=1614556800,v1=abc…,v1=def…".
	timestampElement = "t"
	signatureElement = "v1"

	// providerStripe and providerRevenueCat are the Provider labels the two
	// named constructors report.
	providerStripe     = "stripe"
	providerRevenueCat = "revenuecat"
)

type (
	// TimestampedHMACScheme describes a provider that signs a timestamp and the
	// raw body together — "<timestamp>.<body>" — with HMAC-SHA-256, and sends
	// both the timestamp and the hex MAC as elements of a single header.
	//
	// Stripe published the shape and RevenueCat adopted it verbatim, down to the
	// element keys; only the header name differs between them. That is why this
	// is a scheme with a header rather than two verifiers: the parse, the
	// ordering of the freshness check against the HMAC work, and the exact bytes
	// that get signed are all things a second copy could get subtly wrong, and
	// being wrong here is silent.
	//
	// A provider that signs the body alone is the other shape, and is
	// HMACScheme.
	TimestampedHMACScheme struct {
		// Provider is the label the verifier reports and the receiver stamps on
		// every Delivery. Required.
		Provider string

		// Header names the request header carrying the timestamp and the MAC.
		// Required. Lookup is case-insensitive, as HTTP header lookup always is.
		Header string
	}

	// TimestampedHMACVerifier verifies a t=…,v1=… header against the timestamp
	// and body it signs.
	TimestampedHMACVerifier struct {
		cfg     *verifierConfig
		scheme  TimestampedHMACScheme
		hashers []hashing.Hasher
	}
)

var _ Verifier = (*TimestampedHMACVerifier)(nil)

// NewTimestampedHMACVerifier builds a Verifier for a provider that signs
// "<timestamp>.<body>" under scheme.
//
// Signing the timestamp alongside the body is what makes the freshness check
// meaningful: the value compared against the clock is inside the signed
// material, so an attacker replaying a captured delivery cannot move it.
//
// Reads WithAdditionalSecrets, WithTolerance, WithClock, and
// WithVerificationTime.
//
// A header may carry several v1 elements and any one of them matching is
// enough. Stripe emits one per active endpoint secret during its own secret
// rollover, so rejecting on the first mismatch would fail every delivery for
// the length of a rotation the receiver has no say in.
func NewTimestampedHMACVerifier(scheme *TimestampedHMACScheme, secret string, opts ...VerifierOption) (*TimestampedHMACVerifier, error) {
	if scheme == nil {
		return nil, platformerrors.Wrap(platformerrors.ErrNilInputParameter, "nil timestamped HMAC scheme")
	}

	// Copied before it is read, so a reused literal cannot mean two different
	// things later, and so nothing here edits the caller's descriptor.
	s := *scheme

	if s.Provider == "" {
		return nil, platformerrors.Wrap(platformerrors.ErrEmptyInputParameter, "timestamped HMAC scheme provider")
	}

	if s.Header == "" {
		return nil, platformerrors.Wrap(platformerrors.ErrEmptyInputParameter, "timestamped HMAC scheme header")
	}

	cfg := newVerifierConfig(opts)

	secrets := cfg.secretsWith(secret)
	if len(secrets) == 0 {
		return nil, ErrNoSecret
	}

	hashers := make([]hashing.Hasher, 0, len(secrets))
	for _, key := range secrets {
		hashers = append(hashers, hmac.NewHMACSHA256Hasher([]byte(key)))
	}

	return &TimestampedHMACVerifier{cfg: cfg, scheme: s, hashers: hashers}, nil
}

// NewStripeVerifier builds a Verifier for Stripe's Stripe-Signature header.
//
// secret is the endpoint's signing secret, the "whsec_…" value. Reads
// WithAdditionalSecrets, WithTolerance, WithClock, and WithVerificationTime.
func NewStripeVerifier(secret string, opts ...VerifierOption) (*TimestampedHMACVerifier, error) {
	return NewTimestampedHMACVerifier(&TimestampedHMACScheme{
		Provider: providerStripe,
		Header:   StripeSignatureHeader,
	}, secret, opts...)
}

// NewRevenueCatVerifier builds a Verifier for RevenueCat's
// X-RevenueCat-Webhook-Signature header.
//
// secret is the signing secret shown on the webhook integration in RevenueCat's
// dashboard, which is a different value from the Authorization header
// configured on the same page — see RevenueCatSignatureHeader on why only the
// signed scheme is implemented. Reads WithAdditionalSecrets, WithTolerance,
// WithClock, and WithVerificationTime.
//
// RevenueCat re-signs on every delivery attempt, so the timestamp is when that
// particular request was signed rather than when the event happened. A retry of
// an event from an hour ago therefore arrives inside the tolerance window,
// which is the behavior the freshness check wants: it bounds how long a
// captured request stays replayable, not how old an event may be.
func NewRevenueCatVerifier(secret string, opts ...VerifierOption) (*TimestampedHMACVerifier, error) {
	return NewTimestampedHMACVerifier(&TimestampedHMACScheme{
		Provider: providerRevenueCat,
		Header:   RevenueCatSignatureHeader,
	}, secret, opts...)
}

// Provider returns the scheme's provider label.
func (v *TimestampedHMACVerifier) Provider() string { return v.scheme.Provider }

// Verify checks the scheme's header against body.
//
// The staleness check runs before any HMAC work, so a flood of replayed
// deliveries costs a parse rather than a hash per key. It runs on the
// timestamp as presented, which is unauthenticated at that point — but a
// forged timestamp only ever moves a delivery out of the window or leaves it
// signed under a payload whose MAC will not match, so nothing is decided on an
// unverified value.
func (v *TimestampedHMACVerifier) Verify(_ context.Context, headers http.Header, body []byte) error {
	// A nil header bag reads as an absent header rather than a special case, which is what
	// http.Header.Get already does.
	presented := headers.Get(v.scheme.Header)
	if presented == "" {
		return platformerrors.Wrapf(ErrInvalidSignature, "no %s header", v.scheme.Header)
	}

	sig := parseTimestampedSignature(presented)
	if sig.timestamp.IsZero() || len(sig.candidates) == 0 {
		return platformerrors.Wrapf(ErrInvalidSignature, "malformed %s header", v.scheme.Header)
	}

	if err := v.cfg.Check(sig.timestamp); err != nil {
		return err
	}

	// The signed payload carries the timestamp exactly as it appeared in the
	// header, not a re-rendering of the parsed time: a provider that ever pads
	// or formats it differently signed the bytes it sent, and re-rendering
	// would produce a MAC over bytes nobody signed.
	signed := make([]byte, 0, len(sig.rawTimestamp)+1+len(body))
	signed = append(signed, sig.rawTimestamp...)
	signed = append(signed, '.')
	signed = append(signed, body...)

	// Every secret against every presented v1 element, without
	// short-circuiting; see hmac.MatchesAny.
	if !hmac.MatchesAny(v.hashers, signed, sig.candidates...) {
		return ErrInvalidSignature
	}

	return nil
}

// timestampedSignature is a parsed t=…,v1=… header.
type timestampedSignature struct {
	// rawTimestamp is the t element's text, which is what gets signed.
	rawTimestamp string
	// timestamp is rawTimestamp parsed, which is what gets compared to a clock.
	timestamp time.Time
	// candidates are the decoded v1 elements, any one of which may match.
	candidates [][]byte
}

// parseTimestampedSignature pulls the timestamp and every v1 signature out of a
// timestamped-HMAC header value.
//
// Unrecognized elements are skipped rather than rejected: Stripe has added
// elements to this header before (v0, for its test-mode scheme) and will
// again, and a verifier that failed on an element it did not know would be
// broken by a change that was designed to be backward compatible.
func parseTimestampedSignature(header string) timestampedSignature {
	var sig timestampedSignature

	for element := range strings.SplitSeq(header, ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(element), "=")
		if !ok {
			continue
		}

		switch key {
		case timestampElement:
			seconds, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				return timestampedSignature{}
			}

			sig.rawTimestamp, sig.timestamp = value, time.Unix(seconds, 0)
		case signatureElement:
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
