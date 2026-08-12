/*
Package redis implements ratelimiting.RateLimiter as a sliding window kept in a
Redis sorted set.

# What choosing it commits you to

The counter lives outside the process, which is the reason to pick it: every
replica counts against the same window, so a limit means the same thing however
many instances are serving. That is what an in-process limiter cannot do — its
limit multiplies by the replica count.

Each Allow is a round trip running a Lua script, so the limiter is on the request
path and costs one. The script drops entries older than the window, counts
what is left, and admits by adding a member — all atomically, which is what keeps
two concurrent requests from both reading a count below the limit and both being
allowed.

Every admitted request is stored, not merely counted: a window is a sorted set
holding one member per request in it, so memory scales with the rate and not with
the number of keys alone. Keys are namespaced under "ratelimit:" and expire at
twice the window, so an idle key cleans itself up.

# Failure is reported, not decided

A Redis that cannot be reached makes Allow return an error. It does not fall back
to allowing, and it does not fall back to refusing — whether an outage in the
limiter should open the gate or close it is a policy decision belonging to
whatever is being protected, and this package will not make it silently.

# The window is derived, not configured

The limiter is configured as a rate and a burst, and turns that pair into a
window: up to burst requests per window, where the window is how long a full
burst takes to accrue at the steady rate. Both Allow and RetryAfter read the same
derivation, so the hint always measures against the window the decision was made
in.

RetryAfter reports when the oldest request in a saturated window falls out of it,
and costs another round trip — worth making on the refusal path and not
otherwise. Nothing reserves the slot it describes, so a client that waits exactly
that long may still find it taken: the answer is a floor, not a promise. A window
with room reports no hint at all rather than zero, which would invite a client
back immediately for a token it was never refused.
*/
package redis
