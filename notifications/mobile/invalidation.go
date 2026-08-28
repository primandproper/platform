package mobile

import (
	"context"

	"github.com/primandproper/platform-go/v13/notifications/mobile/internal/pushfeedback"
)

// ErrTokenInvalid marks a send that failed because the device token will never
// receive another notification: the app was uninstalled, the token was reissued,
// or it was minted for a different application entirely.
//
// It is a sentinel wrapped around the provider's own error rather than a
// replacement for it, so a caller reads errors.Is for the classification and
// still gets the provider's words in the message.
//
// The distinction it draws is the one a registry lives or dies by. A push that
// fails because APNs was unreachable is a push to retry; a push that fails
// because the token is dead is a row to delete, and treating the second as the
// first means pushing into the void forever while every send reports a failure
// nobody can act on. The senders in this package's subpackages classify their
// own provider's answers, because the taxonomy is the vendor's — see
// apns.Sender.Send and fcm.Sender.Send.
// It is declared in an internal package and re-exported here rather than
// declared here outright, because the senders that raise it are subpackages and
// this package's own tests construct them — see
// notifications/mobile/internal/pushfeedback. The value is the same one either
// way, which is all errors.Is needs.
var ErrTokenInvalid = pushfeedback.ErrTokenInvalid

// TokenInvalidator is told about a device token a provider has permanently
// rejected, so that the registry holding it can stop addressing pushes to a
// handset that no longer exists.
//
// It takes the platform and token as plain strings, which is what a sender has:
// a provider answering a push names the token it rejected and nothing else — not
// the tenant, not the person, not the row. notifications.Registry implements
// this, and the argument types are why neither package has to import the other.
//
// Implementations must be idempotent. Two workers can be told about the same
// dead token, and a hook that errored on the second would turn a successful
// prune into a logged failure.
type TokenInvalidator interface {
	InvalidateDeviceToken(ctx context.Context, platform, token string) error
}

// WithTokenInvalidator wires provider feedback into a registry: a send that
// fails with ErrTokenInvalid tells the invalidator which token to drop.
//
// Absent, the classification still reaches the caller as an error — nothing is
// hidden — but nothing prunes, which is the state every deployment was in before
// this existed.
func WithTokenInvalidator(invalidator TokenInvalidator) Option {
	return func(o *options) { o.tokenInvalidator = invalidator }
}
