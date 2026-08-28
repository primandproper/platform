/*
Package pushfeedback holds the one sentinel a provider sender marks a
permanently-rejected device token with.

It is a package of one variable, and it exists for an import direction rather
than for a concept. mobile is where a consumer reads the sentinel from — the
exported spelling is mobile.ErrTokenInvalid, beside the TokenInvalidator it
feeds — but mobile's own tests construct apns.Sender and fcm.Sender to exercise
the typed-nil routing, so a sender importing mobile for the sentinel makes that
test an import cycle.

Both sides import this instead, and mobile re-exports the identical value, so
errors.Is compares one variable however a caller reached it. Nothing else
belongs here: a second thing in this package would be a thing whose home was
decided by a cycle.
*/
package pushfeedback

import (
	"github.com/primandproper/platform-go/v13/errors"
)

// ErrTokenInvalid marks a send that failed because the device token will never
// receive another notification. mobile.ErrTokenInvalid is the same value under
// the name a caller uses, and carries the reasoning.
var ErrTokenInvalid = errors.New("the provider will not deliver to this device token again")
