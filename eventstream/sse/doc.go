/*
Package sse upgrades an HTTP request to a Server-Sent Events stream.

It is the send-only half of eventstream's two transports. Upgrader satisfies
eventstream.EventStreamUpgrader and not the bidirectional one, because SSE has
no client-to-server channel: a client that needs to talk back does so with an
ordinary request, or uses the websocket sibling.

Choosing it commits a caller to plain HTTP with no handshake, no subprotocol
negotiation, and no dependency beyond net/http — a stream survives proxies and
corporate middleboxes that refuse an Upgrade. What it costs is listed below.

# The response is committed at upgrade time

UpgradeToEventStream writes the text/event-stream headers and flushes before it
returns, so by the time a handler has a stream it can no longer choose a status
code. Anything that could fail the request has to fail before the upgrade. The
ResponseWriter must also implement http.Flusher, which is checked first and is
the one error the upgrade returns.

# There is no keepalive

Nothing is written when there are no events. A client that has gone away is
discovered on the next Send, and an idle connection is at the mercy of whatever
proxy timeout sits in front of it — this package sends no comment frames and no
pings, and Done fires from the request's own context rather than from any
liveness check. The websocket sibling pings on an interval and closes a stream
whose peer stops answering; that is the substantive difference between the two
for a long-lived, low-traffic stream.

# Framing

An event is written as an optional "event:" line and one "data:" line per line
of payload, terminated by a blank line. CR and CRLF are normalized to LF and the
event type has its newlines stripped, so neither a multi-line payload nor an
attacker-supplied type can break the framing or inject additional SSE fields.

No "id:" field is emitted. A client reconnecting therefore has no Last-Event-ID
to send and no way to ask for what it missed; a stream resumes as a new one from
the present moment.

Close cancels the stream's context so Done fires and the handler unblocks. It
writes nothing to the client, which learns of the close when the response body
ends.
*/
package sse
