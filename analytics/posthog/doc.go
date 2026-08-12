/*
Package posthog reports analytics events to PostHog.

Choosing it commits a deployment to a PostHog project API key and, unless
Config.Endpoint names EU Cloud or a self-hosted instance, to PostHog US Cloud.

# A nil error means buffered, not delivered

AddUser, EventOccurred, and EventOccurredAnonymous hand the event to the PostHog
client's in-memory buffer and return. Nothing about the return value describes
what happened on the network, because at that point nothing has: a background
flush delivers later, and the only report of a delivery that failed is the error
counter and the log line this package records from the client's Failure
callback. That line names the event and its UUID and deliberately omits its
properties, which are the caller's data rather than this service's.

Two things follow. Close is the final flush and its error is the last chance
anyone has to learn that buffered events did not arrive, so a process that exits
without calling it drops whatever was still in the buffer, silently. And the
circuit breaker is driven from those delivery callbacks rather than from the
enqueue, so it tracks whether PostHog is accepting events rather than whether
the buffer accepted them.

The breaker is a required constructor argument rather than an option: while it
is open, every call returns circuitbreaking.ErrCircuitBroken without enqueueing.
Nothing here retries.

# Compared with the segment sibling

WithConfigModifiers reaches the underlying posthog.Config before the client is
built, which is how a caller changes anything this package's own Config does not
name — including the delivery callback, in which case the breaker is no longer
wired to delivery outcomes. The segment implementation has no such escape hatch
and no endpoint setting at all.
*/
package posthog
