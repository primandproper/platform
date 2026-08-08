package requestsigning

import (
	"context"
	"net/http"
	"strconv"

	platformerrors "github.com/primandproper/platform-go/v10/errors"
)

// signer mints v1 signatures over a keyring it re-reads per request.
type signer struct {
	keys KeySource
	cfg  *config
}

var _ Signer = (*signer)(nil)

// NewSigner builds the v1 Signer: it stamps SignatureHeader and
// TimestampHeader over the request body, under every key the source holds.
//
// The keyring is resolved per call rather than captured, so a rotation in the
// secret store reaches the wire without a restart — see NewSecretKeySource for
// what makes that affordable.
//
// Reads WithClock; WithTolerance and WithVerificationTime belong to the
// verifying side and are ignored here.
func NewSigner(keys KeySource, opts ...Option) (Signer, error) {
	if keys == nil {
		return nil, ErrNilKeySource
	}

	return &signer{keys: keys, cfg: newConfig(opts)}, nil
}

// Scheme returns SchemeV1.
func (s *signer) Scheme() string { return SchemeV1 }

// SignHeaders stamps the signature and timestamp headers over body.
//
// The timestamp header carries the same value that is inside the signature. It
// is set separately so a receiver can reject a stale request before spending an
// HMAC on it; a receiver must still treat the signature as authoritative, since
// only the copy inside the signed material is covered by the MAC.
func (s *signer) SignHeaders(ctx context.Context, header http.Header, body []byte) error {
	if header == nil {
		return platformerrors.Wrap(platformerrors.ErrNilInputParameter, "nil header")
	}

	keyring, err := s.keys.Keyring(ctx)
	if err != nil {
		return platformerrors.Wrap(err, "resolving the signing keyring")
	}

	at := s.cfg.now().UTC()

	signature, err := Sign(keyring, body, at)
	if err != nil {
		return err
	}

	header.Set(SignatureHeader, signature)
	header.Set(TimestampHeader, strconv.FormatInt(at.Unix(), 10))

	return nil
}
