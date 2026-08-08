package requestsigning

import (
	"context"
	"net/http"
	"time"

	platformerrors "github.com/primandproper/platform-go/v10/errors"
)

const (
	// SignatureHeader carries the signature(s) over the request body.
	SignatureHeader = "X-Platform-Signature"

	// TimestampHeader carries the signing timestamp, as Unix seconds. It is the
	// same value that appears inside the signature; it is exposed separately so
	// a receiver can reject a stale request before doing any HMAC work.
	TimestampHeader = "X-Platform-Timestamp"

	// SchemeV1 is the only scheme this package mints. It is the literal prefix
	// bound into the signed bytes, so changing it is a wire break, not a rename.
	SchemeV1 = "v1"

	// DefaultTolerance is how far a signature's timestamp may sit from the
	// verifier's clock before verification rejects it.
	//
	// Five minutes is the customary figure, and it is a compromise between two
	// real failures: too tight and ordinary clock skew between sender and
	// receiver rejects good requests, too loose and a captured request stays
	// replayable for as long as the window lasts.
	DefaultTolerance = 5 * time.Minute
)

var (
	// ErrInvalidSignature indicates a signature header that is missing,
	// malformed, carries no recognized scheme, or does not match the body under
	// any key the verifier holds. The cases are deliberately one error: telling
	// a caller which of them applied tells an attacker how close a forgery came.
	ErrInvalidSignature = platformerrors.New("invalid request signature")

	// ErrStaleSignature indicates a signature whose timestamp is outside the
	// tolerance. It is distinct from ErrInvalidSignature because it is the one
	// verification failure with a benign cause an operator can act on — clock
	// skew — and it says nothing about the key.
	ErrStaleSignature = platformerrors.New("request signature timestamp outside tolerance")

	// ErrNoSigningKey indicates a keyring with no current key. Unsigned
	// requests are not something this package will mint: a receiver that cannot
	// authenticate a payload cannot safely act on it.
	ErrNoSigningKey = platformerrors.New("no current signing key")

	// ErrNoVerificationKey indicates a verification attempted against a keyring
	// holding no keys at all.
	//
	// It is deliberately not ErrInvalidSignature. A verifier with no keys
	// rejects everything, which looks from the outside exactly like a fleet of
	// callers that all got their signing wrong; naming it separately is what
	// lets the server report its own misconfiguration as a fault of its own
	// rather than as a verdict about the caller.
	ErrNoVerificationKey = platformerrors.New("no verification key")

	// ErrNilKeySource indicates a constructor called without a KeySource. It
	// wraps errors.ErrNilInputParameter, so a caller may check either.
	ErrNilKeySource = platformerrors.Wrap(platformerrors.ErrNilInputParameter, "nil signing key source")
)

// Keyring carries the HMAC keys a signature is minted and checked under.
//
// It is a pair rather than a single value so that rotation is not an outage. A
// request is signed under Current and, while Previous is set, again under
// Previous; both signatures travel in the same header. A receiver therefore
// accepts requests throughout the window in which either side is switching
// keys, and the operator clears Previous once everyone has moved.
//
// A single shared secret makes that impossible: rolling it breaks every
// counterparty at the same instant, so in practice it never gets rolled.
type Keyring struct {
	// Current is the key new signatures are minted under. Required to sign.
	Current []byte `json:"-"`
	// Previous is an outgoing key still emitted alongside Current during a
	// rotation window. Empty outside one.
	Previous []byte `json:"-"`
}

// Keys reports the keyring's non-empty keys, in the order a signature emits
// them.
func (k Keyring) Keys() [][]byte {
	keys := make([][]byte, 0, 2)

	for _, key := range [][]byte{k.Current, k.Previous} {
		if len(key) > 0 {
			keys = append(keys, key)
		}
	}

	return keys
}

type (
	// Signer stamps the headers that prove a request body was produced by
	// someone holding the key.
	//
	// It is an interface rather than a function so that a scheme reading its key
	// from somewhere per call is expressible without every caller learning its
	// shape. Neither half of this pair takes an *http.Request: what a signature
	// covers is bytes and headers, and a seam that took a request would have to
	// have an opinion about who reads the body and what bounds that read. Those
	// are serving concerns, and they live in requestsigning/http and in
	// httpclient, where a request already means something.
	Signer interface {
		// Scheme names the wire format this signer mints. It is a label, for
		// spans and log lines; nothing dispatches on it.
		Scheme() string

		// SignHeaders writes whatever proves body was signed into header.
		//
		// It writes a bag rather than returning one value because a scheme may
		// carry more than one — v1 sets a timestamp beside its signature, so a
		// receiver can shed a stale request before hashing anything. Only the
		// signature is authoritative, which is why the verifying half below
		// reads a single value.
		//
		// It is called once per attempt rather than once per logical request, so
		// the timestamp it stamps is always fresh — a retry that fires after a
		// long backoff must not arrive already stale.
		SignHeaders(ctx context.Context, header http.Header, body []byte) error
	}

	// Verifier checks that a request body was signed by a holder of a key it
	// trusts, and is the inbound half of Signer.
	//
	// The per-provider inbound schemes — Stripe's t=…,v1=…, GitHub's
	// X-Hub-Signature-256 — are implementations of this interface rather than
	// separate verification stacks, so a service verifying a third party's
	// webhooks and a service verifying its own first-party callers run the same
	// middleware over the same seam.
	Verifier interface {
		// Scheme names the wire format this verifier reads. It is a label, for
		// spans and log lines; nothing dispatches on it.
		Scheme() string

		// HeaderName is the request header this scheme's proof travels in, so a
		// caller knows what to hand to VerifyHeaderValue without knowing the
		// scheme. It is fixed for the life of the verifier.
		HeaderName() string

		// VerifyHeaderValue checks body against the proof carried in value, and
		// returns nil only if it matches. An empty value is ErrInvalidSignature:
		// an unsigned request and a badly signed one are both "did not prove it
		// holds the key".
		//
		// body must be the exact bytes received, read before any decoding.
		// Decoding and re-encoding changes key order and whitespace, and the
		// signature covers bytes, not meaning.
		VerifyHeaderValue(ctx context.Context, value string, body []byte) error
	}
)
