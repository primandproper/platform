/*
Package clock provides an injectable source of time so components that stamp,
pace, or schedule work can be tested deterministically.

The Clock interface covers the three ways services consume time: reading it
(Now, Since), pacing against it (Sleep, which is context-aware and never
strands a goroutine past cancellation), and ticking on it (NewTicker).
NewClock returns the production implementation backed by the time package;
clock/fake provides a manually-advanced Clock for tests, so time-dependent
logic — TTL expiry, backoff pacing, periodic sweeps — can be exercised in
nanoseconds of wall time instead of real sleeps.

Components should accept a Clock rather than calling time.Now or time.Sleep
directly. Scheduling (cron, distributed coordination) is out of scope; in a
multi-process system the shared database's clock, not this one, is the
arbiter of ordering.

# Relationship to testing/synctest

The wall Clock delegates to the time package on every call, so inside a
testing/synctest bubble it automatically rides the bubble's fake clock —
test code under synctest should use NewClock (or the component's injected
production clock), not clock/fake. Reach for clock/fake when a test cannot
run in a bubble (it does real I/O, e.g. against containers) or when it needs
time to move only at explicit points rather than auto-advancing whenever the
bubble goes idle.
*/
package clock
