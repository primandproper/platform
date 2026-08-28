package fcm

import (
	"errors"
	"testing"

	platformerrors "github.com/primandproper/platform-go/v13/errors"
	"github.com/primandproper/platform-go/v13/notifications/mobile/internal/pushfeedback"

	"github.com/shoenig/test"
)

// errArbitrary stands in for a send that failed for a reason the token has
// nothing to do with.
var errArbitrary = platformerrors.New("the provider is unreachable")

// TestTokenIsDead covers the direction that can be constructed from outside
// firebase: everything that is not one of the SDK's two token classifications.
//
// That is the direction with the expensive failure. Over-classifying is how a
// registry gets emptied — INVALID_ARGUMENT is Firebase's answer for a malformed
// payload as well as a malformed token, so treating it as fatal would
// unregister every handset in the deployment the first time somebody shipped a
// bad message.
func TestTokenIsDead(T *testing.T) {
	T.Parallel()

	T.Run("an ordinary failure is not a dead token", func(t *testing.T) {
		t.Parallel()

		test.False(t, tokenIsDead(errArbitrary))
		test.False(t, tokenIsDead(platformerrors.Wrap(errArbitrary, "sending")))
		test.False(t, tokenIsDead(nil))
	})
}

// TestMarkDeadToken is the other half, and it is a separate function precisely
// so that it can be tested: a genuine UNREGISTERED can only be built by
// firebase's own internal package, so the classification is passed in.
func TestMarkDeadToken(T *testing.T) {
	T.Parallel()

	T.Run("a classified failure is marked", func(t *testing.T) {
		t.Parallel()

		err := markDeadToken(errArbitrary, true)

		test.ErrorIs(t, err, pushfeedback.ErrTokenInvalid)

		// Marked, not replaced: the SDK's error is what says which of the two
		// happened, and a caller reading the message wants it.
		test.StrContains(t, err.Error(), errArbitrary.Error())
	})

	T.Run("an unclassified failure is left alone", func(t *testing.T) {
		t.Parallel()

		err := markDeadToken(errArbitrary, false)

		test.ErrorIs(t, err, errArbitrary)
		test.False(t, errors.Is(err, pushfeedback.ErrTokenInvalid))
	})

	T.Run("a successful send is left alone", func(t *testing.T) {
		t.Parallel()

		test.NoError(t, markDeadToken(nil, true))
	})
}
