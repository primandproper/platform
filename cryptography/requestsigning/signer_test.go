package requestsigning

import (
	"context"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/primandproper/platform-go/v10/clock"
	clockmock "github.com/primandproper/platform-go/v10/clock/mock"
	platformerrors "github.com/primandproper/platform-go/v10/errors"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// fixedClock reads one instant forever, which is what a signer needs from a
// clock in a test: the assertion is about what got stamped, not about time
// passing.
func fixedClock(at time.Time) clock.Clock {
	return &clockmock.ClockMock{NowFunc: func() time.Time { return at }}
}

func TestNewSigner(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		signer, err := NewSigner(StaticKeyring(Keyring{Current: []byte("secret")}), WithClock(fixedClock(signingTime)))
		must.NoError(t, err)

		test.EqOp(t, SchemeV1, signer.Scheme())

		header := http.Header{}
		must.NoError(t, signer.SignHeaders(t.Context(), header, testBody))

		// What was stamped is what Verify accepts, which is the only property
		// worth asserting about a signer.
		test.NoError(t, Verify(
			Keyring{Current: []byte("secret")},
			testBody,
			header.Get(SignatureHeader),
			WithVerificationTime(signingTime),
		))

		// The timestamp header is the same instant as the one inside the
		// signature, so a receiver that sheds stale requests before hashing
		// cannot disagree with the one that hashes.
		test.EqOp(t, strconv.FormatInt(signingTime.Unix(), 10), header.Get(TimestampHeader))
	})

	// Each call reads the clock again. This is what makes a retry that fires
	// after a long backoff arrive fresh rather than stale.
	T.Run("stamps a fresh timestamp per call", func(t *testing.T) {
		t.Parallel()

		now := signingTime
		signer, err := NewSigner(
			StaticKeyring(Keyring{Current: []byte("secret")}),
			WithClock(&clockmock.ClockMock{NowFunc: func() time.Time { return now }}),
		)
		must.NoError(t, err)

		first := http.Header{}
		must.NoError(t, signer.SignHeaders(t.Context(), first, testBody))

		now = signingTime.Add(time.Hour)

		second := http.Header{}
		must.NoError(t, signer.SignHeaders(t.Context(), second, testBody))

		test.NotEqOp(t, first.Get(SignatureHeader), second.Get(SignatureHeader))
		test.EqOp(t, strconv.FormatInt(now.Unix(), 10), second.Get(TimestampHeader))
	})

	// The keyring is read per request, so a rotation in the store reaches the
	// wire without a restart.
	T.Run("re-reads the keyring per request", func(t *testing.T) {
		t.Parallel()

		key := []byte("first")

		signer, err := NewSigner(
			KeySourceFunc(func(context.Context) (Keyring, error) { return Keyring{Current: key}, nil }),
			WithClock(fixedClock(signingTime)),
		)
		must.NoError(t, err)

		key = []byte("second")

		header := http.Header{}
		must.NoError(t, signer.SignHeaders(t.Context(), header, testBody))

		test.NoError(t, Verify(
			Keyring{Current: []byte("second")},
			testBody,
			header.Get(SignatureHeader),
			WithVerificationTime(signingTime),
		))
	})

	T.Run("reports a key source it could not read", func(t *testing.T) {
		t.Parallel()

		boom := platformerrors.New("the store is down")

		signer, err := NewSigner(KeySourceFunc(func(context.Context) (Keyring, error) { return Keyring{}, boom }))
		must.NoError(t, err)

		test.ErrorIs(t, signer.SignHeaders(t.Context(), http.Header{}, testBody), boom)
	})

	T.Run("reports a keyring with no current key", func(t *testing.T) {
		t.Parallel()

		signer, err := NewSigner(StaticKeyring(Keyring{Previous: []byte("old")}))
		must.NoError(t, err)

		test.ErrorIs(t, signer.SignHeaders(t.Context(), http.Header{}, testBody), ErrNoSigningKey)
	})

	T.Run("rejects its own bad inputs", func(t *testing.T) {
		t.Parallel()

		_, err := NewSigner(nil)
		test.ErrorIs(t, err, ErrNilKeySource)

		signer, err := NewSigner(StaticKeyring(Keyring{Current: []byte("k")}))
		must.NoError(t, err)

		test.ErrorIs(t, signer.SignHeaders(t.Context(), nil, testBody), platformerrors.ErrNilInputParameter)
	})
}
