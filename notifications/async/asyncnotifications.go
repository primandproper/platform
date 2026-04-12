package async

import (
	"context"
	"encoding/json"
	"net/http"
)

// Event represents an async notification event to be published to a channel.
type Event struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

// AsyncNotifier publishes events to named channels.
// Implementations may deliver via WebSocket, SSE, Pusher, Ably, or other backends.
type AsyncNotifier interface {
	// Publish sends an event to all subscribers of the given channel.
	Publish(ctx context.Context, channel string, event *Event) error
	// Close releases resources held by the notifier.
	Close() error
}

// ConnectionAcceptor is an optional interface implemented by backends that
// require server-side HTTP connection management (WebSocket, SSE).
// Callers may type-assert an AsyncNotifier to ConnectionAcceptor when they
// need to accept inbound client connections.
type ConnectionAcceptor interface {
	// AcceptConnection upgrades an HTTP request to a persistent connection
	// and registers it under the given channel and memberID.
	// The connection is managed internally; events published to the channel
	// will be delivered to this connection.
	AcceptConnection(w http.ResponseWriter, r *http.Request, channel, memberID string) error
}
