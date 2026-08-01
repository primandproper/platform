package sse

import (
	"context"
	"net/http"

	"github.com/primandproper/platform-go/v9/eventstream"
	essse "github.com/primandproper/platform-go/v9/eventstream/sse"
	"github.com/primandproper/platform-go/v9/notifications/async"
	"github.com/primandproper/platform-go/v9/observability"
	"github.com/primandproper/platform-go/v9/observability/keys"
)

const o11yName = "async_notifications_sse"

var (
	_ async.AsyncNotifier      = (*Notifier)(nil)
	_ async.ConnectionAcceptor = (*Notifier)(nil)
)

// Notifier is an SSE-backed AsyncNotifier that manages direct client connections.
// Note that AcceptConnection blocks the calling goroutine for the lifetime of the
// client connection, as SSE uses the HTTP response writer directly.
type Notifier struct {
	o11y     observability.Observer
	upgrader *essse.Upgrader
	manager  *eventstream.StreamManager[eventstream.EventStream]
}

// NewNotifier creates a new SSE-backed AsyncNotifier.
func NewNotifier(_ *Config, opts ...Option) (*Notifier, error) {
	o := newOptions(opts)

	return &Notifier{
		o11y:     observability.NewObserver(o11yName, o.logger, o.tracerProvider),
		upgrader: essse.NewUpgrader(essse.WithTracerProvider(o.tracerProvider)),
		manager:  eventstream.NewStreamManager[eventstream.EventStream](eventstream.WithTracerProvider(o.tracerProvider), eventstream.WithLogger(o.logger)),
	}, nil
}

// Publish sends an event to all connected clients on the given channel.
func (n *Notifier) Publish(ctx context.Context, channel string, event *async.Event) error {
	ctx, op := n.o11y.Begin(ctx,
		observability.WithValue(keys.TopicKey, channel),
		observability.WithValue("event.type", event.Type),
		observability.WithValue(keys.LengthKey, len(event.Data)),
	)
	defer op.End()

	esEvent := &eventstream.Event{
		Type:    event.Type,
		Payload: event.Data,
	}

	n.manager.BroadcastToGroup(ctx, channel, esEvent)

	return nil
}

// AcceptConnection upgrades the HTTP connection to an SSE stream and registers it
// under the given channel and memberID. This method blocks the calling goroutine
// for the lifetime of the client connection.
func (n *Notifier) AcceptConnection(w http.ResponseWriter, r *http.Request, channel, memberID string) error {
	ctx, op := n.o11y.Begin(r.Context())
	defer op.End()

	op.Set(keys.TopicKey, channel).Set("member_id", memberID)

	stream, err := n.upgrader.UpgradeToEventStream(w, r)
	if err != nil {
		return op.Error(err, "upgrading SSE connection")
	}

	n.manager.Add(ctx, channel, memberID, stream)

	defer func(removeCtx context.Context) {
		n.manager.Remove(removeCtx, channel, memberID)
	}(ctx)

	// Block until the client disconnects.
	<-stream.Done()

	return nil
}

// Close releases resources held by the notifier.
func (n *Notifier) Close() error {
	return nil
}
