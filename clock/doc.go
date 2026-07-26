/*
Package clock provides an injectable source of time so components that stamp,
pace, or schedule work can be tested deterministically.

The Clock interface covers the three ways services consume time: reading it
(Now, Since), pacing against it (Sleep, which is context-aware and never
strands a goroutine past cancellation), and ticking on it (NewTicker).
NewClock returns the production implementation backed by the time package.

Components should accept a Clock rather than calling time.Now or time.Sleep
directly. Scheduling (cron, distributed coordination) is out of scope; in a
multi-process system the shared database's clock, not this one, is the
arbiter of ordering.

# Testing time-dependent logic

There is no fake Clock, because testing/synctest makes one unnecessary. The
wall Clock delegates to the time package on every call and caches nothing, so
inside a bubble it rides the bubble's fake clock: a component under test keeps
its production Clock, and the test moves time with time.Sleep. TTL expiry,
backoff pacing, and periodic sweeps run in nanoseconds of wall time. See
synctest_test.go, which pins this contract.

The bubble replaces the two things a hand-rolled fake provided. Advancing is
time.Sleep in the test goroutine, or nothing at all — when every goroutine is
durably blocked, time jumps to the next deadline on its own. Waiting for a
goroutine to reach its sleep or ticker before advancing is synctest.Wait,
which needs no count of registered waiters and returns without moving time.

That auto-advance is worth respecting, because it will happily rescue a
broken test. A blocking receive on the result of the code under test —
<-done, <-ticked — durably blocks the bubble, so time skips to whatever
deadline comes next and the receive succeeds no matter how wrong the interval
was. Such a test passes with the cadence set to an hour. Assert timing the
other way instead: sleep to just short of the deadline, synctest.Wait (which
parks everything without moving the clock), and check that nothing has
happened; then step across the deadline, Wait again, and check with a
non-blocking receive or a counter that exactly one thing did. Pair it with a
deferred cancel or Close so a failed assertion unwinds the goroutines under
test and reports itself instead of tripping the bubble's deadlock panic.

Two limits are worth knowing. A bubble's clock always starts at midnight UTC
2000-01-01, so a test wanting a particular timestamp derives it from
time.Now inside the bubble. And Example functions cannot open a bubble —
synctest.Test needs a *testing.T — so an example that must not be interrupted
by a tick sets an interval longer than the example instead.

Tests that genuinely cannot run in a bubble — those blocking on real network
or container I/O, which never counts as durably blocked — should use real
durations against the real clock, as the integration suites do.
*/
package clock
