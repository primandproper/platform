package mobile

import "context"

// PushMessage holds the content of a push notification.
// BadgeCount is optional; when non-nil on iOS, sets the app icon badge.
type PushMessage struct {
	BadgeCount *int
	Title      string
	Body       string
}

// PushNotificationSender sends push notifications to device tokens.
// Implementations route by platform: APNs for ios, FCM for android.
type PushNotificationSender interface {
	// SendPush sends a push notification to a single device token.
	// platform is "ios" or "android"; implementations filter by platform.
	SendPush(ctx context.Context, platform, token string, msg PushMessage) error
}
