package requestsigning

import (
	"context"

	platformerrors "github.com/primandproper/platform-go/v10/errors"
)

// verifier checks v1 signatures against a keyring it re-reads per request.
type verifier struct {
	keys KeySource
	cfg  *config
}

var _ Verifier = (*verifier)(nil)

// NewVerifier builds the v1 Verifier: it checks the SignatureHeader value
// against the body under every key the source holds.
//
// The keyring is resolved per call, so a receiver picks up the far side's key
// rotation without a restart — and, more usefully, can carry both keys through
// a window of its own.
//
// Reads WithClock, WithTolerance, and WithVerificationTime.
func NewVerifier(keys KeySource, opts ...Option) (Verifier, error) {
	if keys == nil {
		return nil, ErrNilKeySource
	}

	return &verifier{keys: keys, cfg: newConfig(opts)}, nil
}

// Scheme returns SchemeV1.
func (v *verifier) Scheme() string { return SchemeV1 }

// HeaderName returns SignatureHeader.
//
// TimestampHeader is deliberately not named here. It is a courtesy copy for a
// receiver that wants to shed a stale request before reading a body at all; the
// value this scheme checks is the one inside the signed material, which is the
// only one an attacker cannot edit.
func (v *verifier) HeaderName() string { return SignatureHeader }

// VerifyHeaderValue checks a SignatureHeader value against body.
func (v *verifier) VerifyHeaderValue(ctx context.Context, value string, body []byte) error {
	if value == "" {
		// The same error a wrong signature gets. An unsigned request and a
		// badly signed one are both "did not prove it holds the key", and
		// separating them tells a prober which header this endpoint reads.
		return platformerrors.Wrapf(ErrInvalidSignature, "no %s header", SignatureHeader)
	}

	keyring, err := v.keys.Keyring(ctx)
	if err != nil {
		return platformerrors.Wrap(err, "resolving the verification keyring")
	}

	// Through the same code Verify runs, against a configuration resolved once
	// at construction. The object and the function cannot drift on what a
	// tolerance or a clock means, because there is only one of each.
	return verify(keyring, body, value, v.cfg)
}
