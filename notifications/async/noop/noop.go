package noop

import (
	"context"

	"github.com/primandproper/platform/notifications/async"
	"github.com/primandproper/platform/observability/logging"
)

var _ async.AsyncNotifier = (*asyncNotifier)(nil)

// asyncNotifier is a no-op implementation of AsyncNotifier.
type asyncNotifier struct {
	logger logging.Logger
}

// NewAsyncNotifier returns a new no-op AsyncNotifier.
func NewAsyncNotifier() (async.AsyncNotifier, error) {
	return &asyncNotifier{logger: logging.NewNoopLogger()}, nil
}

// Publish is a no-op.
func (n *asyncNotifier) Publish(context.Context, string, *async.Event) error {
	n.logger.Info("NoopAsyncNotifier.Publish: no-op")
	return nil
}

// Close is a no-op.
func (n *asyncNotifier) Close() error {
	return nil
}
