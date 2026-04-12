package analytics

import (
	"context"
)

type (
	// EventReporter collects data about customers.
	EventReporter interface {
		Close()
		AddUser(ctx context.Context, userID string, properties map[string]any) error
		EventOccurred(ctx context.Context, event, userID string, properties map[string]any) error
		EventOccurredAnonymous(ctx context.Context, event, anonymousID string, properties map[string]any) error
	}
)
