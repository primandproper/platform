package requestsigning

import (
	"context"
	"net/http"
	"testing"
	"time"

	platformerrors "github.com/primandproper/platform-go/v10/errors"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// signedHeader renders the header a signer would have produced, so the verifier
// tests state what arrives rather than how it was built.
func signedHeader(t *testing.T, keyring Keyring, body []byte, at time.Time) http.Header {
	t.Helper()

	signature, err := Sign(keyring, body, at)
	must.NoError(t, err)

	header := http.Header{}
	header.Set(SignatureHeader, signature)

	return header
}

func TestNewVerifier(T *testing.T) {
	T.Parallel()

	keyring := Keyring{Current: []byte("secret")}

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		verifier, err := NewVerifier(StaticKeyring(keyring), WithVerificationTime(signingTime))
		must.NoError(t, err)

		test.EqOp(t, SchemeV1, verifier.Scheme())
		test.NoError(t, verifier.VerifyRequest(t.Context(), signedHeader(t, keyring, testBody, signingTime), testBody))
	})

	// A missing header is the same answer as a wrong one. Separating them tells
	// a prober which header this endpoint reads.
	T.Run("an unsigned request is an invalid one", func(t *testing.T) {
		t.Parallel()

		verifier, err := NewVerifier(StaticKeyring(keyring), WithVerificationTime(signingTime))
		must.NoError(t, err)

		test.ErrorIs(t,
			verifier.VerifyRequest(t.Context(), http.Header{}, testBody),
			ErrInvalidSignature,
		)
	})

	T.Run("rejects a tampered body", func(t *testing.T) {
		t.Parallel()

		verifier, err := NewVerifier(StaticKeyring(keyring), WithVerificationTime(signingTime))
		must.NoError(t, err)

		test.ErrorIs(t,
			verifier.VerifyRequest(t.Context(), signedHeader(t, keyring, testBody, signingTime), []byte("something else")),
			ErrInvalidSignature,
		)
	})

	T.Run("honors the configured tolerance", func(t *testing.T) {
		t.Parallel()

		header := signedHeader(t, keyring, testBody, signingTime)

		tight, err := NewVerifier(StaticKeyring(keyring),
			WithVerificationTime(signingTime.Add(30*time.Minute)))
		must.NoError(t, err)

		test.ErrorIs(t, tight.VerifyRequest(t.Context(), header, testBody), ErrStaleSignature)

		loose, err := NewVerifier(StaticKeyring(keyring),
			WithVerificationTime(signingTime.Add(30*time.Minute)),
			WithTolerance(time.Hour))
		must.NoError(t, err)

		test.NoError(t, loose.VerifyRequest(t.Context(), header, testBody))
	})

	// The verifier re-reads its keyring too, so a receiver picks up its own
	// rotation without a restart.
	T.Run("re-reads the keyring per request", func(t *testing.T) {
		t.Parallel()

		trusted := Keyring{Current: []byte("first")}

		verifier, err := NewVerifier(
			KeySourceFunc(func(context.Context) (Keyring, error) { return trusted, nil }),
			WithVerificationTime(signingTime),
		)
		must.NoError(t, err)

		second := Keyring{Current: []byte("second")}
		header := signedHeader(t, second, testBody, signingTime)

		test.ErrorIs(t, verifier.VerifyRequest(t.Context(), header, testBody), ErrInvalidSignature)

		trusted = second

		test.NoError(t, verifier.VerifyRequest(t.Context(), header, testBody))
	})

	T.Run("reports a key source it could not read", func(t *testing.T) {
		t.Parallel()

		boom := platformerrors.New("the store is down")

		verifier, err := NewVerifier(KeySourceFunc(func(context.Context) (Keyring, error) { return Keyring{}, boom }))
		must.NoError(t, err)

		test.ErrorIs(t,
			verifier.VerifyRequest(t.Context(), signedHeader(t, keyring, testBody, time.Now()), testBody),
			boom,
		)
	})

	T.Run("rejects its own bad inputs", func(t *testing.T) {
		t.Parallel()

		_, err := NewVerifier(nil)
		test.ErrorIs(t, err, ErrNilKeySource)

		verifier, err := NewVerifier(StaticKeyring(keyring))
		must.NoError(t, err)

		test.ErrorIs(t, verifier.VerifyRequest(t.Context(), nil, testBody), platformerrors.ErrNilInputParameter)
	})
}

// A signer and a verifier built from the same key source agree, which is the
// property every other test in this package is a special case of.
func TestSignerVerifierRoundTrip(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		keys := StaticKeyring(Keyring{Current: []byte("shared")})

		signer, err := NewSigner(keys, WithClock(fixedClock(signingTime)))
		must.NoError(t, err)

		verifier, err := NewVerifier(keys, WithVerificationTime(signingTime))
		must.NoError(t, err)

		header := http.Header{}
		must.NoError(t, signer.SignRequest(t.Context(), header, testBody))

		test.NoError(t, verifier.VerifyRequest(t.Context(), header, testBody))
	})
}
