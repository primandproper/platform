package mobile

// MobileNotificationRequest is the generic message payload for mobile push notifications.
// RequestType determines which handler processes the request; schedulers format the message.
type MobileNotificationRequest struct {
	Context          map[string]string `json:"context,omitempty"`
	BadgeCount       *int              `json:"badgeCount,omitempty"`
	RequestType      string            `json:"requestType"`
	Title            string            `json:"title"`
	Body             string            `json:"body"`
	TestID           string            `json:"testID,omitempty"`
	RecipientUserIDs []string          `json:"recipientUserIDs"`
}
