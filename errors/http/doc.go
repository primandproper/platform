/*
Package http translates errors into HTTP responses, in both directions.

The forward direction is a Go error to the response a client sees: ToAPIError
resolves it to an ErrorCode and a safe message, HTTPStatusForCode turns that code
into a status, and ToAPIResponse does both and builds the envelope. The reverse
direction is ErrorForCode, which a typed client uses to turn a code it received
back into the platform sentinel it stands for — so a caller of a remote service
matches ratelimiting.ErrRateLimited with errors.Is exactly as it would inside the
serving process. Only the codes that came from exactly one sentinel invert;
everything else reports nil, because guessing would hand a client an error to
branch on that nobody promised.

# Which direction the imports run

This package imports the packages whose sentinels it maps — circuitbreaking,
database, idempotency, links, ratelimiting, sessions, and the rest. That is what
lets the mapping live in one place instead of being restated at every handler.
It also fixes the dependency direction: nothing in those packages may import
errors/http back. A package that finds itself wanting an ErrorCode wants a
sentinel of its own, mapped here.

Domains outside this module register their own mappers with
RegisterHTTPErrorMapper, usually from an init function. The platform mapper is
consulted first, registered mappers after, in registration order.

# Messages are deliberately uninformative

An unmatched error resolves to the neutral code and "an error occurred" — never
to a domain-specific code, so a failure inside a payment path cannot surface as
one blaming the database. Matched errors get a fixed message that names no limit,
no permission, and no quota: those are useful to an operator and equally useful
to someone probing an endpoint for the shape of the policy behind it. What the
handler already knows it can render itself, next to an upgrade link or in a log,
rather than putting it on the wire.

Unmapped codes resolve to 500. That is the direction to fail in: a code nobody
gave a status keeps its server-side failure looking like one.
*/
package http
