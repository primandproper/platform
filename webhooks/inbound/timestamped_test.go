package inbound

import (
	"fmt"
	"strings"
	"testing"
	"time"

	clockmock "github.com/primandproper/platform-go/v13/clock/mock"
	"github.com/primandproper/platform-go/v13/cryptography/hashing"
	"github.com/primandproper/platform-go/v13/cryptography/hashing/hmac"
	platformerrors "github.com/primandproper/platform-go/v13/errors"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// signedAt is the instant every timestamped-HMAC test signs at and verifies against, so
// nothing here depends on how long the test took to run.
var signedAt = time.Unix(1614556800, 0)

// timestampedHeader renders a t=…,v1=… header signing body at ts under each secret. It is
// the same rendering for every provider on this scheme, which is the fact the scheme exists
// to record.
func timestampedHeader(ts time.Time, body []byte, secrets ...string) string {
	seconds := fmt.Sprintf("%d", ts.Unix())

	elements := []string{"t=" + seconds}
	for _, secret := range secrets {
		elements = append(elements, "v1="+hashing.HexString(
			hmac.NewHMACSHA256Hasher([]byte(secret)),
			seconds+"."+string(body),
		))
	}

	return strings.Join(elements, ",")
}

func TestNewStripeVerifier(T *testing.T) {
	T.Parallel()

	body := []byte(`{"id":"evt_123","type":"payment_intent.succeeded"}`)

	newVerifier := func(t *testing.T, secret string, opts ...VerifierOption) Verifier {
		t.Helper()

		verifier, err := NewStripeVerifier(secret, append([]VerifierOption{WithVerificationTime(signedAt)}, opts...)...)
		must.NoError(t, err)

		return verifier
	}

	T.Run("verifies a signature over the timestamp and body", func(t *testing.T) {
		t.Parallel()

		verifier := newVerifier(t, "whsec_test")

		test.EqOp(t, providerStripe, verifier.Provider())
		test.NoError(t, verifier.Verify(
			t.Context(),
			signedHeaders(StripeSignatureHeader, timestampedHeader(signedAt, body, "whsec_test")),
			body,
		))
	})

	// The signature is over "<timestamp>.<body>", not over the body alone. A verifier that
	// dropped the timestamp from the signed material would still be checking a MAC under the
	// right key, and would accept a delivery whose timestamp had been rewritten.
	T.Run("rejects a signature that omits the timestamp from the signed payload", func(t *testing.T) {
		t.Parallel()

		verifier := newVerifier(t, "whsec_test")

		bodyOnly := hashing.Hex(hmac.NewHMACSHA256Hasher([]byte("whsec_test")), body)
		header := fmt.Sprintf("t=%d,v1=%s", signedAt.Unix(), bodyOnly)

		test.ErrorIs(t, verifier.Verify(t.Context(), signedHeaders(StripeSignatureHeader, header), body), ErrInvalidSignature)
	})

	// Stripe emits one v1 per active endpoint secret while it rolls its own. Only one of them
	// is ours, and rejecting on the first mismatch would fail every delivery for the length of
	// a rotation this receiver has no say in.
	T.Run("accepts any one of several v1 signatures", func(t *testing.T) {
		t.Parallel()

		verifier := newVerifier(t, "whsec_test")

		header := timestampedHeader(signedAt, body, "whsec_someone_else", "whsec_test", "whsec_also_not_ours")

		test.NoError(t, verifier.Verify(t.Context(), signedHeaders(StripeSignatureHeader, header), body))
	})

	T.Run("accepts a signature under an additional secret", func(t *testing.T) {
		t.Parallel()

		verifier := newVerifier(t, "whsec_incoming", WithAdditionalSecrets("whsec_outgoing"))

		for _, secret := range []string{"whsec_incoming", "whsec_outgoing"} {
			header := timestampedHeader(signedAt, body, secret)

			test.NoError(t, verifier.Verify(t.Context(), signedHeaders(StripeSignatureHeader, header), body))
		}
	})

	// Stripe has added elements to this header before (v0, for its test-mode scheme) and will
	// again. Failing on an unknown element would break on a change designed to be compatible.
	T.Run("skips elements it does not recognize", func(t *testing.T) {
		t.Parallel()

		verifier := newVerifier(t, "whsec_test")

		header := timestampedHeader(signedAt, body, "whsec_test") + ",v0=deadbeef,junk"

		test.NoError(t, verifier.Verify(t.Context(), signedHeaders(StripeSignatureHeader, header), body))
	})

	T.Run("skips a v1 element that is not hex", func(t *testing.T) {
		t.Parallel()

		verifier := newVerifier(t, "whsec_test")

		header := "v1=not-hex," + timestampedHeader(signedAt, body, "whsec_test")

		test.NoError(t, verifier.Verify(t.Context(), signedHeaders(StripeSignatureHeader, header), body))
	})

	T.Run("rejects a body it did not sign", func(t *testing.T) {
		t.Parallel()

		verifier := newVerifier(t, "whsec_test")

		header := timestampedHeader(signedAt, body, "whsec_test")

		test.ErrorIs(t,
			verifier.Verify(t.Context(), signedHeaders(StripeSignatureHeader, header), append(body, ' ')),
			ErrInvalidSignature,
		)
	})

	T.Run("rejects a signature under a secret it does not hold", func(t *testing.T) {
		t.Parallel()

		verifier := newVerifier(t, "whsec_test")

		header := timestampedHeader(signedAt, body, "whsec_someone_else")

		test.ErrorIs(t, verifier.Verify(t.Context(), signedHeaders(StripeSignatureHeader, header), body), ErrInvalidSignature)
	})

	T.Run("rejects a malformed or absent header", func(t *testing.T) {
		t.Parallel()

		verifier := newVerifier(t, "whsec_test")

		for name, header := range map[string]string{
			"empty":             "",
			"no elements":       "gibberish",
			"no timestamp":      "v1=deadbeef",
			"unparsed t":        "t=whenever,v1=deadbeef",
			"no v1":             fmt.Sprintf("t=%d", signedAt.Unix()),
			"only unknown keys": fmt.Sprintf("t=%d,v0=deadbeef", signedAt.Unix()),
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				test.ErrorIs(t,
					verifier.Verify(t.Context(), signedHeaders(StripeSignatureHeader, header), body),
					ErrInvalidSignature,
				)
			})
		}

		test.ErrorIs(t, verifier.Verify(t.Context(), nil, body), ErrInvalidSignature)
	})

	// The whole point of signing a timestamp: a captured delivery stops being replayable once
	// the window closes.
	T.Run("rejects a delivery outside the tolerance", func(t *testing.T) {
		t.Parallel()

		verifier := newVerifier(t, "whsec_test")

		for name, ts := range map[string]time.Time{
			"too old":       signedAt.Add(-DefaultTolerance - time.Second),
			"too far ahead": signedAt.Add(DefaultTolerance + time.Second),
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				header := timestampedHeader(ts, body, "whsec_test")

				test.ErrorIs(t,
					verifier.Verify(t.Context(), signedHeaders(StripeSignatureHeader, header), body),
					ErrStaleSignature,
				)
			})
		}
	})

	T.Run("honors a widened tolerance", func(t *testing.T) {
		t.Parallel()

		header := timestampedHeader(signedAt.Add(-time.Hour), body, "whsec_test")
		headers := signedHeaders(StripeSignatureHeader, header)

		test.ErrorIs(t, newVerifier(t, "whsec_test").Verify(t.Context(), headers, body), ErrStaleSignature)
		test.NoError(t, newVerifier(t, "whsec_test", WithTolerance(2*time.Hour)).Verify(t.Context(), headers, body))
	})

	T.Run("refuses to build without a secret", func(t *testing.T) {
		t.Parallel()

		verifier, err := NewStripeVerifier("")

		test.ErrorIs(t, err, ErrNoSecret)
		test.Nil(t, verifier)
	})

	// Nothing pins the verifier to the epoch or to a clock it was not given.
	T.Run("reads the injected clock when no time is pinned", func(t *testing.T) {
		t.Parallel()

		verifier, err := NewStripeVerifier("whsec_test", WithClock(&clockmock.ClockMock{
			NowFunc: func() time.Time { return signedAt },
		}))
		must.NoError(t, err)

		header := timestampedHeader(signedAt, body, "whsec_test")

		test.NoError(t, verifier.Verify(t.Context(), signedHeaders(StripeSignatureHeader, header), body))
	})
}

func TestParseTimestampedSignature(T *testing.T) {
	T.Parallel()

	T.Run("tolerates whitespace around elements", func(t *testing.T) {
		t.Parallel()

		sig := parseTimestampedSignature(" t=1614556800 , v1=deadbeef ")

		test.EqOp(t, "1614556800", sig.rawTimestamp)
		test.EqOp(t, signedAt.Unix(), sig.timestamp.Unix())
		must.SliceLen(t, 1, sig.candidates)
	})

	T.Run("abandons a header whose timestamp does not parse", func(t *testing.T) {
		t.Parallel()

		sig := parseTimestampedSignature("t=nope,v1=deadbeef")

		test.True(t, sig.timestamp.IsZero())
		test.SliceLen(t, 0, sig.candidates)
	})
}

func TestNewTimestampedHMACVerifier(T *testing.T) {
	T.Parallel()

	scheme := &TimestampedHMACScheme{Provider: "acme", Header: "X-Acme-Signature"}

	T.Run("refuses a nil scheme", func(t *testing.T) {
		t.Parallel()

		verifier, err := NewTimestampedHMACVerifier(nil, "sekrit")

		test.ErrorIs(t, err, platformerrors.ErrNilInputParameter)
		test.Nil(t, verifier)
	})

	T.Run("refuses a scheme naming no provider", func(t *testing.T) {
		t.Parallel()

		verifier, err := NewTimestampedHMACVerifier(&TimestampedHMACScheme{Header: "X-Acme-Signature"}, "sekrit")

		test.ErrorIs(t, err, platformerrors.ErrEmptyInputParameter)
		test.Nil(t, verifier)
	})

	T.Run("refuses a scheme naming no header", func(t *testing.T) {
		t.Parallel()

		verifier, err := NewTimestampedHMACVerifier(&TimestampedHMACScheme{Provider: "acme"}, "sekrit")

		test.ErrorIs(t, err, platformerrors.ErrEmptyInputParameter)
		test.Nil(t, verifier)
	})

	T.Run("does not edit the caller's scheme", func(t *testing.T) {
		t.Parallel()

		// The descriptor is copied before it is read, so a literal a caller keeps and
		// reuses cannot be changed underneath them by a constructor.
		given := *scheme

		_, err := NewTimestampedHMACVerifier(&given, "sekrit")
		must.NoError(t, err)

		test.Eq(t, *scheme, given)
	})

	T.Run("verifies an arbitrary provider on the same scheme", func(t *testing.T) {
		t.Parallel()

		body := []byte(`{"whatever":true}`)

		verifier, err := NewTimestampedHMACVerifier(scheme, "sekrit", WithVerificationTime(signedAt))
		must.NoError(t, err)

		test.EqOp(t, "acme", verifier.Provider())
		test.NoError(t, verifier.Verify(
			t.Context(),
			signedHeaders(scheme.Header, timestampedHeader(signedAt, body, "sekrit")),
			body,
		))
	})
}

func TestNewRevenueCatVerifier(T *testing.T) {
	T.Parallel()

	// A RevenueCat delivery, in the envelope shape the capitalism adapter decodes.
	body := []byte(`{"api_version":"1.0","event":{"id":"evt_rc","type":"RENEWAL"}}`)

	newVerifier := func(t *testing.T, secret string, opts ...VerifierOption) Verifier {
		t.Helper()

		verifier, err := NewRevenueCatVerifier(secret, append([]VerifierOption{WithVerificationTime(signedAt)}, opts...)...)
		must.NoError(t, err)

		return verifier
	}

	T.Run("verifies a signature over the timestamp and body", func(t *testing.T) {
		t.Parallel()

		verifier := newVerifier(t, "rcsec_test")

		test.EqOp(t, providerRevenueCat, verifier.Provider())
		test.NoError(t, verifier.Verify(
			t.Context(),
			signedHeaders(RevenueCatSignatureHeader, timestampedHeader(signedAt, body, "rcsec_test")),
			body,
		))
	})

	T.Run("reads its own header rather than Stripe's", func(t *testing.T) {
		t.Parallel()

		// The two schemes are identical apart from the header name, so the header is
		// the only thing keeping a delivery from one provider out of the other's
		// verifier. A correctly signed RevenueCat body presented under
		// Stripe-Signature is not a RevenueCat delivery.
		header := timestampedHeader(signedAt, body, "rcsec_test")

		test.ErrorIs(t,
			newVerifier(t, "rcsec_test").Verify(t.Context(), signedHeaders(StripeSignatureHeader, header), body),
			ErrInvalidSignature,
		)
	})

	T.Run("rejects a body that was not the one signed", func(t *testing.T) {
		t.Parallel()

		header := timestampedHeader(signedAt, body, "rcsec_test")

		test.ErrorIs(t,
			newVerifier(t, "rcsec_test").Verify(t.Context(), signedHeaders(RevenueCatSignatureHeader, header), append(body, ' ')),
			ErrInvalidSignature,
		)
	})

	T.Run("rejects a delivery signed under another secret", func(t *testing.T) {
		t.Parallel()

		header := timestampedHeader(signedAt, body, "rcsec_other")

		test.ErrorIs(t,
			newVerifier(t, "rcsec_test").Verify(t.Context(), signedHeaders(RevenueCatSignatureHeader, header), body),
			ErrInvalidSignature,
		)
	})

	T.Run("rejects a stale delivery", func(t *testing.T) {
		t.Parallel()

		// RevenueCat re-signs each attempt, so a timestamp outside tolerance is a
		// captured request being replayed rather than a retry of an old event.
		header := timestampedHeader(signedAt.Add(-time.Hour), body, "rcsec_test")

		test.ErrorIs(t,
			newVerifier(t, "rcsec_test").Verify(t.Context(), signedHeaders(RevenueCatSignatureHeader, header), body),
			ErrStaleSignature,
		)
	})

	T.Run("survives a secret rotation", func(t *testing.T) {
		t.Parallel()

		header := timestampedHeader(signedAt, body, "rcsec_old")

		test.NoError(t, newVerifier(t, "rcsec_new", WithAdditionalSecrets("rcsec_old")).
			Verify(t.Context(), signedHeaders(RevenueCatSignatureHeader, header), body))
	})

	T.Run("refuses to build without a secret", func(t *testing.T) {
		t.Parallel()

		verifier, err := NewRevenueCatVerifier("")

		test.ErrorIs(t, err, ErrNoSecret)
		test.Nil(t, verifier)
	})
}
