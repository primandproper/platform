package webhooks

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

var (
	signingTime = time.Unix(1753900000, 0).UTC()
	testBody    = []byte(`{"id":"abc","amount":42}`)
)

func TestSign(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		signature, err := Sign(Secret{Current: []byte("secret")}, testBody, signingTime)
		must.NoError(t, err)

		test.True(t, strings.HasPrefix(signature, "v1,t=1753900000,s="))
		test.EqOp(t, 1, strings.Count(signature, ",s="))
	})

	// The signature is deterministic given the same key, body, and time, which
	// is what makes it verifiable at all.
	T.Run("is deterministic", func(t *testing.T) {
		t.Parallel()

		secret := Secret{Current: []byte("secret")}

		first, err := Sign(secret, testBody, signingTime)
		must.NoError(t, err)

		second, err := Sign(secret, testBody, signingTime)
		must.NoError(t, err)

		test.EqOp(t, first, second)
	})

	// The timestamp is inside the signed material, so the same body signed a
	// second later produces a different MAC. This is the property that stops a
	// captured request from being replayable forever.
	T.Run("timestamp changes the signature", func(t *testing.T) {
		t.Parallel()

		secret := Secret{Current: []byte("secret")}

		first, err := Sign(secret, testBody, signingTime)
		must.NoError(t, err)

		second, err := Sign(secret, testBody, signingTime.Add(time.Second))
		must.NoError(t, err)

		test.NotEqOp(t, first, second)
	})

	T.Run("emits both signatures during a rotation", func(t *testing.T) {
		t.Parallel()

		signature, err := Sign(
			Secret{Current: []byte("new"), Previous: []byte("old")},
			testBody, signingTime,
		)
		must.NoError(t, err)

		test.EqOp(t, 2, strings.Count(signature, ",s="))

		// And each component is the signature that key alone would produce.
		current, err := Sign(Secret{Current: []byte("new")}, testBody, signingTime)
		must.NoError(t, err)

		previous, err := Sign(Secret{Current: []byte("old")}, testBody, signingTime)
		must.NoError(t, err)

		test.True(t, strings.Contains(signature, mustSuffix(t, current)))
		test.True(t, strings.Contains(signature, mustSuffix(t, previous)))
	})

	T.Run("without a current secret", func(t *testing.T) {
		t.Parallel()

		_, err := Sign(Secret{Previous: []byte("old")}, testBody, signingTime)
		test.ErrorIs(t, err, ErrNoSigningSecret)
	})
}

func TestVerify(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		secret := Secret{Current: []byte("secret")}

		signature, err := Sign(secret, testBody, signingTime)
		must.NoError(t, err)

		test.NoError(t, Verify(secret, testBody, signature, WithVerificationTime(signingTime)))
	})

	T.Run("rejects a tampered body", func(t *testing.T) {
		t.Parallel()

		secret := Secret{Current: []byte("secret")}

		signature, err := Sign(secret, testBody, signingTime)
		must.NoError(t, err)

		err = Verify(secret, []byte(`{"id":"abc","amount":999}`), signature, WithVerificationTime(signingTime))
		test.ErrorIs(t, err, ErrInvalidSignature)
	})

	T.Run("rejects the wrong secret", func(t *testing.T) {
		t.Parallel()

		signature, err := Sign(Secret{Current: []byte("secret")}, testBody, signingTime)
		must.NoError(t, err)

		err = Verify(Secret{Current: []byte("other")}, testBody, signature, WithVerificationTime(signingTime))
		test.ErrorIs(t, err, ErrInvalidSignature)
	})

	// The point of the rotation window: a subscriber holding the new key accepts
	// a delivery still signed with the old one, and vice versa.
	T.Run("accepts either side of a rotation", func(t *testing.T) {
		t.Parallel()

		sent, err := Sign(Secret{Current: []byte("new"), Previous: []byte("old")}, testBody, signingTime)
		must.NoError(t, err)

		// A subscriber that has already moved to the new key.
		test.NoError(t, Verify(Secret{Current: []byte("new")}, testBody, sent, WithVerificationTime(signingTime)))

		// A subscriber that has not yet moved.
		test.NoError(t, Verify(Secret{Current: []byte("old")}, testBody, sent, WithVerificationTime(signingTime)))

		// One that never had either.
		test.ErrorIs(t,
			Verify(Secret{Current: []byte("unrelated")}, testBody, sent, WithVerificationTime(signingTime)),
			ErrInvalidSignature,
		)
	})

	T.Run("a verifier mid-rotation accepts a sender that has not rotated", func(t *testing.T) {
		t.Parallel()

		sent, err := Sign(Secret{Current: []byte("old")}, testBody, signingTime)
		must.NoError(t, err)

		test.NoError(t, Verify(
			Secret{Current: []byte("new"), Previous: []byte("old")},
			testBody, sent, WithVerificationTime(signingTime),
		))
	})

	// Replay protection. A captured request is valid inside the tolerance and
	// worthless outside it, in both directions — a future timestamp is skew or
	// forgery, not freshness.
	T.Run("rejects a stale signature", func(t *testing.T) {
		t.Parallel()

		secret := Secret{Current: []byte("secret")}

		signature, err := Sign(secret, testBody, signingTime)
		must.NoError(t, err)

		test.NoError(t, Verify(secret, testBody, signature,
			WithVerificationTime(signingTime.Add(4*time.Minute))))

		test.ErrorIs(t,
			Verify(secret, testBody, signature, WithVerificationTime(signingTime.Add(6*time.Minute))),
			ErrStaleSignature,
		)

		test.ErrorIs(t,
			Verify(secret, testBody, signature, WithVerificationTime(signingTime.Add(-6*time.Minute))),
			ErrStaleSignature,
		)
	})

	T.Run("honors a custom tolerance", func(t *testing.T) {
		t.Parallel()

		secret := Secret{Current: []byte("secret")}

		signature, err := Sign(secret, testBody, signingTime)
		must.NoError(t, err)

		test.NoError(t, Verify(secret, testBody, signature,
			WithVerificationTime(signingTime.Add(30*time.Minute)),
			WithTolerance(time.Hour),
		))

		test.ErrorIs(t,
			Verify(secret, testBody, signature,
				WithVerificationTime(signingTime.Add(2*time.Second)),
				WithTolerance(time.Second),
			),
			ErrStaleSignature,
		)
	})

	// A non-positive tolerance leaves the default in place rather than disabling
	// the check, which would make every signature replayable forever.
	T.Run("a non-positive tolerance does not disable the check", func(t *testing.T) {
		t.Parallel()

		secret := Secret{Current: []byte("secret")}

		signature, err := Sign(secret, testBody, signingTime)
		must.NoError(t, err)

		test.ErrorIs(t,
			Verify(secret, testBody, signature,
				WithVerificationTime(signingTime.Add(time.Hour)),
				WithTolerance(0),
			),
			ErrStaleSignature,
		)
	})

	// A v1 signature must not verify as anything else, and an unknown scheme
	// must be refused rather than ignored.
	T.Run("rejects an unknown scheme", func(t *testing.T) {
		t.Parallel()

		secret := Secret{Current: []byte("secret")}

		signature, err := Sign(secret, testBody, signingTime)
		must.NoError(t, err)

		swapped := "v2" + strings.TrimPrefix(signature, "v1")

		test.ErrorIs(t,
			Verify(secret, testBody, swapped, WithVerificationTime(signingTime)),
			ErrInvalidSignature,
		)
	})

	T.Run("malformed headers", func(t *testing.T) {
		t.Parallel()

		secret := Secret{Current: []byte("secret")}

		for name, signature := range map[string]string{
			"empty":               "",
			"scheme only":         "v1",
			"no timestamp":        "v1,s=abcd",
			"no signature":        "v1,t=1753900000",
			"unparseable time":    "v1,t=soon,s=abcd",
			"non-hex signature":   "v1,t=1753900000,s=zzzz",
			"component with no =": "v1,t=1753900000,s=abcd,garbage",
			"duplicate timestamp": "v1,t=1753900000,t=1753900001,s=abcd",
			"too many components": "v1,t=1" + strings.Repeat(",s=ab", 20),
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				err := Verify(secret, testBody, signature, WithVerificationTime(signingTime))
				must.Error(t, err)
				test.True(t, errors.Is(err, ErrInvalidSignature))
			})
		}
	})

	// An empty body is a legal payload for the signing construction even though
	// Dispatch refuses to send one — the two rejections belong at different
	// layers, and Verify must not disagree with Sign about it.
	T.Run("empty body round-trips", func(t *testing.T) {
		t.Parallel()

		secret := Secret{Current: []byte("secret")}

		signature, err := Sign(secret, nil, signingTime)
		must.NoError(t, err)

		test.NoError(t, Verify(secret, nil, signature, WithVerificationTime(signingTime)))
	})
}

// mustSuffix returns the s=<hex> component of a single-signature header, for
// asserting that a rotating header contains it.
func mustSuffix(t *testing.T, signature string) string {
	t.Helper()

	_, suffix, found := strings.Cut(signature, ",s=")
	must.True(t, found)

	return ",s=" + suffix
}

func TestSigningPayload(T *testing.T) {
	T.Parallel()

	// Pinned literally, because this is the wire contract every subscriber
	// reimplements. A change here breaks every consumer at once and must be a
	// new scheme version rather than an edit.
	T.Run("renders scheme.timestamp.body", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t,
			`v1.1753900000.{"id":"abc","amount":42}`,
			string(signingPayload(SignatureSchemeV1, 1753900000, testBody)),
		)
	})
}
