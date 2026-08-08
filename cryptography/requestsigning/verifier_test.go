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

// signedValue renders the header value a signer would have produced, so the
// verifier tests state what arrives rather than how it was built.
func signedValue(t *testing.T, keyring Keyring, body []byte, at time.Time) string {
	t.Helper()

	signature, err := Sign(keyring, body, at)
	must.NoError(t, err)

	return signature
}

func TestNewVerifier(T *testing.T) {
	T.Parallel()

	keyring := Keyring{Current: []byte("secret")}

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		verifier, err := NewVerifier(StaticKeyring(keyring), WithVerificationTime(signingTime))
		must.NoError(t, err)

		test.EqOp(t, SchemeV1, verifier.Scheme())
		test.NoError(t, verifier.VerifyHeaderValue(t.Context(), signedValue(t, keyring, testBody, signingTime), testBody))
	})

	// The caller reads the header this names, so a scheme's header is the
	// scheme's business rather than something every wiring site restates.
	T.Run("names the header it reads", func(t *testing.T) {
		t.Parallel()

		verifier, err := NewVerifier(StaticKeyring(keyring))
		must.NoError(t, err)

		test.EqOp(t, SignatureHeader, verifier.HeaderName())
	})

	// A missing header is the same answer as a wrong one. Separating them tells
	// a prober which header this endpoint reads.
	T.Run("an unsigned request is an invalid one", func(t *testing.T) {
		t.Parallel()

		verifier, err := NewVerifier(StaticKeyring(keyring), WithVerificationTime(signingTime))
		must.NoError(t, err)

		test.ErrorIs(t, verifier.VerifyHeaderValue(t.Context(), "", testBody), ErrInvalidSignature)
	})

	T.Run("rejects a tampered body", func(t *testing.T) {
		t.Parallel()

		verifier, err := NewVerifier(StaticKeyring(keyring), WithVerificationTime(signingTime))
		must.NoError(t, err)

		test.ErrorIs(t,
			verifier.VerifyHeaderValue(t.Context(), signedValue(t, keyring, testBody, signingTime), []byte("something else")),
			ErrInvalidSignature,
		)
	})

	T.Run("honors the configured tolerance", func(t *testing.T) {
		t.Parallel()

		value := signedValue(t, keyring, testBody, signingTime)

		tight, err := NewVerifier(StaticKeyring(keyring),
			WithVerificationTime(signingTime.Add(30*time.Minute)))
		must.NoError(t, err)

		test.ErrorIs(t, tight.VerifyHeaderValue(t.Context(), value, testBody), ErrStaleSignature)

		loose, err := NewVerifier(StaticKeyring(keyring),
			WithVerificationTime(signingTime.Add(30*time.Minute)),
			WithTolerance(time.Hour))
		must.NoError(t, err)

		test.NoError(t, loose.VerifyHeaderValue(t.Context(), value, testBody))
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
		value := signedValue(t, second, testBody, signingTime)

		test.ErrorIs(t, verifier.VerifyHeaderValue(t.Context(), value, testBody), ErrInvalidSignature)

		trusted = second

		test.NoError(t, verifier.VerifyHeaderValue(t.Context(), value, testBody))
	})

	T.Run("reports a key source it could not read", func(t *testing.T) {
		t.Parallel()

		boom := platformerrors.New("the store is down")

		verifier, err := NewVerifier(KeySourceFunc(func(context.Context) (Keyring, error) { return Keyring{}, boom }))
		must.NoError(t, err)

		test.ErrorIs(t,
			verifier.VerifyHeaderValue(t.Context(), signedValue(t, keyring, testBody, time.Now()), testBody),
			boom,
		)
	})

	T.Run("rejects its own bad inputs", func(t *testing.T) {
		t.Parallel()

		_, err := NewVerifier(nil)
		test.ErrorIs(t, err, ErrNilKeySource)
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
		must.NoError(t, signer.SignHeaders(t.Context(), header, testBody))

		// The verifier names the header, so the two halves are wired together
		// without either side's caller restating the scheme's wire details.
		test.NoError(t, verifier.VerifyHeaderValue(t.Context(), header.Get(verifier.HeaderName()), testBody))
	})
}
