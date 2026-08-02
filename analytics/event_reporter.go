package analytics

import (
	"context"
)

type (
	// EventReporter collects data about customers.
	EventReporter interface {
		// Close flushes whatever is buffered and releases the reporter.
		//
		// It takes a context and returns an error because the flush is a network
		// call that can time out or fail: without them, a process exiting with a
		// full buffer drops those events and has no way to notice.
		Close(ctx context.Context) error
		AddUser(ctx context.Context, userID string, properties map[string]any) error
		EventOccurred(ctx context.Context, event, userID string, properties map[string]any) error
		EventOccurredAnonymous(ctx context.Context, event, anonymousID string, properties map[string]any) error
	}
)
