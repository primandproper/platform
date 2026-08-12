/*
Package websocket upgrades an HTTP request to a WebSocket event stream, over
gorilla/websocket.

Upgrader satisfies both eventstream.EventStreamUpgrader and
eventstream.BidirectionalEventStreamUpgrader, so it is the transport to choose
when the client needs to send as well as receive; the sse sibling cannot. Each
event is one JSON-encoded frame.

# Liveness is active

With a heartbeat interval configured — 30s by default — the stream pings on that
interval and sets a read deadline of one and a half intervals, refreshed by each
pong. A peer that stops answering fails the deadline, the stream closes, Done
fires, and anything holding the stream can deregister it. Writes carry their own
10s deadline.

That is what a caller is buying over SSE, which writes nothing between events
and so discovers a dead peer only on the next send. Setting HeartbeatInterval to
zero turns it off and gives up that property.

A send-only stream still reads, because gorilla processes control frames on the
read path: without a reader, pongs are never seen and closes are never noticed.

# Origins

CheckOrigin is derived from Config.AllowedOrigins. Empty leaves gorilla's default
in place, which permits same-origin requests only — a safe default, and one that
refuses a browser client served from a different host. A non-empty list permits
exactly those Origin header values, and permits requests with no Origin at all,
since non-browser clients do not send one and cannot be judged by it.

# Bidirectional streams

Receive delivers inbound events over a 64-slot buffered channel, closed when the
read loop ends. A frame that does not parse as an eventstream.Event is logged and
discarded rather than delivered or fatal — malformed input from a client should
not take the connection down, but a client sending nothing and a client sending
garbage would otherwise look identical from the server.

The buffer bounds how far a slow consumer can fall behind: once it is full the
read loop blocks, which stops draining the socket and eventually applies
backpressure to the client rather than growing memory here. A caller that does
not read Receive gets that after 64 events.
*/
package websocket
