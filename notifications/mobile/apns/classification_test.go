package apns

import (
	"testing"

	"github.com/primandproper/platform-go/v13/notifications/mobile/internal/pushfeedback"
	"github.com/primandproper/platform-go/v13/observability"

	"github.com/shoenig/test"
	"github.com/sideshow/apns2"
)

// TestTokenIsDead is the line between a token to delete and a configuration to
// fix, and it is worth pinning because both sides are 4xx responses from Apple.
//
// Widening it is how a registry gets emptied: BadTopic is this sender's own
// misconfiguration and would be the answer for every token it holds, so
// classifying it as fatal to the token would unregister every handset in the
// deployment on the first bad deploy.
func TestTokenIsDead(T *testing.T) {
	T.Parallel()

	T.Run("the token is what Apple is refusing", func(t *testing.T) {
		t.Parallel()

		for _, reason := range []string{
			apns2.ReasonBadDeviceToken,
			apns2.ReasonUnregistered,
			apns2.ReasonExpiredToken,
			apns2.ReasonDeviceTokenNotForTopic,
		} {
			test.True(t, tokenIsDead(reason), test.Sprintf("%q", reason))
		}
	})

	T.Run("the request around it is not the token", func(t *testing.T) {
		t.Parallel()

		for _, reason := range []string{
			"",
			apns2.ReasonBadTopic,
			apns2.ReasonBadPriority,
			apns2.ReasonPayloadTooLarge,
			apns2.ReasonTooManyRequests,
			apns2.ReasonInternalServerError,
			apns2.ReasonExpiredProviderToken,
		} {
			test.False(t, tokenIsDead(reason), test.Sprintf("%q", reason))
		}
	})
}

// TestSend_MalformedTokenIsInvalid pins the client-side half of the same
// classification. A value that is not an APNs token will never become one, so a
// registry holding it should drop it on the signal it drops a retired token —
// and this is the one rejection that never reaches Apple.
func TestSend_MalformedTokenIsInvalid(t *testing.T) {
	t.Parallel()

	s := &Sender{o11y: observability.NewObserver(o11yName, nil, nil)}

	err := s.Send(t.Context(), "not-a-token", "title", "body", nil)
	test.ErrorIs(t, err, pushfeedback.ErrTokenInvalid)
}
