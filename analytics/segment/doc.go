/*
Package segment reports analytics events to Segment.

Choosing it commits a deployment to a Segment write key and to Segment's own
hosted endpoint: Config carries the token and nothing else, so unlike the
posthog sibling there is no host to point at a different region, and no option
reaches the underlying segment.Config to add one.

What a caller gets in exchange is fan-out. Every Track and Identify goes out
with all integrations enabled, so an event published here reaches every
destination the Segment workspace has turned on, and adding a destination is a
change made in Segment rather than in this deployment.

# A nil error means buffered, not delivered

AddUser, EventOccurred, and EventOccurredAnonymous append to the client's
in-memory buffer and return. A background flush delivers later, and the only
report of a delivery that failed is the error counter and the log line this
package records from the client's Failure callback — which names the message
and its ID, and omits its properties and traits, since those are the caller's
data rather than this service's.

Close is therefore the final flush, and its error is the last chance anyone has
to learn that buffered events did not arrive. A process that exits without it
drops whatever was still buffered.

The circuit breaker is a required constructor argument and is driven from those
delivery callbacks rather than from the enqueue, so it reflects whether Segment
is accepting events rather than whether the buffer accepted them. While it is
open, every call returns circuitbreaking.ErrCircuitBroken without enqueueing.
Nothing here retries.
*/
package segment
