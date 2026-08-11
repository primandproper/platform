package noop

import (
	"context"

	"github.com/primandproper/platform-go/v10/analytics"
)

var _ analytics.EventReporter = (*EventReporter)(nil)

// EventReporter is a no-op EventReporter.
type EventReporter struct{}

// NewEventReporter returns a new no-op EventReporter.
func NewEventReporter() *EventReporter {
	return &EventReporter{}
}

// Close does nothing.
func (c *EventReporter) Close(context.Context) error {
	return nil
}

// AddUser does nothing.
func (c *EventReporter) AddUser(context.Context, string, map[string]any) error {
	return nil
}

// EventOccurred does nothing.
func (c *EventReporter) EventOccurred(context.Context, string, string, map[string]any) error {
	return nil
}

// EventOccurredAnonymous does nothing.
func (c *EventReporter) EventOccurredAnonymous(context.Context, string, string, map[string]any) error {
	return nil
}
