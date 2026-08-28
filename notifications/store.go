package notifications

import (
	"context"

	"github.com/primandproper/platform-go/v13/filtering"
	"github.com/primandproper/platform-go/v13/tenancy"
)

// Two seams rather than one, and the split is the difference in lifecycle.
//
// An inbox row is created, read, and archived: it is a durable record of what
// somebody was told, and it outlives the delivery. A device token is registered
// and then either revoked by its owner or destroyed on the provider's word: it
// is a routing fact with no history worth keeping, and a soft delete would leave
// rows every send path has to remember to exclude.
//
// They are separate interfaces because they have separate consumers. A service
// with an in-app inbox and no mobile app implements one; a service that pushes
// to handsets and shows nothing in-app implements the other. One interface
// carrying both would make each of those a set of methods somebody has to stub.
//
// [SQLStore] implements both against one schema, so adopting them together is
// one constructor and one migration.

// Inbox is the persistence seam for in-app notifications.
//
// This package ships a SQL implementation ([NewSQLStore]) together with the DDL
// it needs (notifications/migrations), so adopting it does not mean writing this.
// The interface exists because an inbox and its storage are genuinely separable,
// and an application with its own schema conventions should not have to fork the
// package to keep them.
//
// Every method takes a tenancy.Scope and a principal, and none of them offers a
// variant that omits either — an implementation must filter on both rather than
// treat them as hints. The scope is the tenancy doctrine; the principal is this
// package's own, and it is load-bearing for the same reason: a notification
// addressed to somebody is not a row the rest of their tenant may read, so a
// read keyed on the scope alone would let any member of an account read any
// other member's inbox by id.
type Inbox interface {
	// CreateNotification files one notification, under the scope and principal
	// the value carries. It assigns the id where the caller left it empty, and
	// writes back what was stored.
	CreateNotification(ctx context.Context, notification *Notification) error

	// GetNotification reads one of the principal's live notifications. It
	// returns an error wrapping ErrNotificationNotFound when the notification
	// does not exist, has been archived, or belongs to somebody else — which are
	// the same answer from here.
	GetNotification(ctx context.Context, scope tenancy.Scope, principal, notificationID string) (*Notification, error)

	// ListNotifications pages the principal's inbox, in the direction the filter
	// names.
	ListNotifications(ctx context.Context, scope tenancy.Scope, principal string, filter *filtering.QueryFilter) (*filtering.QueryFilteredResult[Notification], error)

	// ListUnreadNotifications is ListNotifications restricted to what the
	// principal has not read.
	//
	// It is a separate method rather than a flag, because unread is "read_at is
	// absent" and there is no value a caller could pass to relax it. The badge
	// count every client asks for first is on the result's pagination: the
	// filtered count is of everything unread, not of the page.
	ListUnreadNotifications(ctx context.Context, scope tenancy.Scope, principal string, filter *filtering.QueryFilter) (*filtering.QueryFilteredResult[Notification], error)

	// MarkNotificationRead stamps one notification as read, now.
	//
	// It is idempotent and does not move the stamp: a notification the principal
	// has already read reports success and keeps the time it was first read,
	// which is what a digest and a re-notify both read. A notification that is
	// not in the inbox — archived, absent, or somebody else's — is an error
	// wrapping ErrNotificationNotFound.
	MarkNotificationRead(ctx context.Context, scope tenancy.Scope, principal, notificationID string) error

	// MarkAllNotificationsRead stamps everything the principal has not read, and
	// reports how many that was.
	//
	// The count is the answer rather than a diagnostic: it is what was sitting
	// unread at the moment the statement ran, and there is no cheaper way to
	// learn it.
	MarkAllNotificationsRead(ctx context.Context, scope tenancy.Scope, principal string) (int64, error)

	// ArchiveNotification dismisses one notification, leaving the row for
	// whoever asks later what somebody was told.
	//
	// A notification already archived is an error wrapping
	// ErrNotificationNotFound, because an archived notification is not in the
	// inbox and this method addresses the inbox.
	ArchiveNotification(ctx context.Context, scope tenancy.Scope, principal, notificationID string) error
}

// Registry is the persistence seam for device tokens: what a push is addressed
// to, and the half of this package that goes wrong quietly.
//
// A provider reports an invalid or expired token on send, and a registry that
// never prunes them keeps pushing into the void while reporting success. That
// feedback loop is why the registry lives beside the senders rather than in the
// consumer: [Registry.InvalidateDeviceToken] is the hook
// notifications/mobile calls with what the provider said, and a token it removes
// is gone rather than flagged.
type Registry interface {
	// RegisterDevice records a device token under the scope and principal the
	// value carries, and writes back what was stored.
	//
	// It converges on (platform, token) rather than inserting, because the token
	// is the handset and a handset re-registers on every app launch and every
	// token rotation. A token already registered to somebody else moves to the
	// principal registering it now, keeping its id and its creation time: a
	// handset that changes hands has one owner, and a registry that kept both
	// would deliver the previous owner's notifications to the new one.
	//
	// It assigns the id and the last-seen time where the caller left them unset,
	// and fills the value with the row that is there afterwards — which for a
	// re-registration is the original id and creation time rather than whatever
	// the caller was holding.
	RegisterDevice(ctx context.Context, device *Device) error

	// ListDevices pages the principal's registered devices.
	ListDevices(ctx context.Context, scope tenancy.Scope, principal string, filter *filtering.QueryFilter) (*filtering.QueryFilteredResult[Device], error)

	// ListDevicesByPrincipals reads every device registered to any of the named
	// principals, in one query.
	//
	// It is the read a fan-out makes: a notification addressed to thirty members
	// of an account is thirty inbox rows and one query for the tokens to push
	// to, rather than thirty round trips returning two rows each. An empty set of
	// principals is an empty answer and no query.
	ListDevicesByPrincipals(ctx context.Context, scope tenancy.Scope, principals []string) ([]*Device, error)

	// RevokeDevice removes one of the principal's registrations — a sign-out, or
	// a device somebody no longer has. The row is deleted rather than archived.
	// A registration that is not there is an error wrapping ErrDeviceNotFound.
	RevokeDevice(ctx context.Context, scope tenancy.Scope, principal, deviceID string) error

	// InvalidateDeviceToken removes a token the provider has permanently
	// rejected, whoever it belongs to.
	//
	// It is the one method here that takes no scope, and the omission is the
	// point rather than an oversight. What a provider hands back is a token: APNs
	// answers a push with Unregistered, FCM with UNREGISTERED, and neither knows
	// or reports which tenant's directory the handset was registered under. A
	// scoped variant would need the caller to already know the answer this
	// exists to act on, and a sender that guessed wrong would leave the dead
	// token in place. The token identifies one row across the whole registry —
	// see the unique index in notifications/migrations — so naming it is naming
	// exactly one device.
	//
	// It is idempotent: a token already gone is the state the caller asked for,
	// and it reports no error. That matters because the caller is a send path,
	// and a push must not fail because two workers pruned the same dead token.
	//
	// It takes the platform and token as plain strings, which is what
	// notifications/mobile.TokenInvalidator requires and what makes a Registry
	// wirable into a sender without either package importing the other.
	InvalidateDeviceToken(ctx context.Context, platform, token string) error
}
