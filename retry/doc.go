/*
Package retry provides retry policies for resilient operation execution.

The root package holds the Policy seam and the vocabulary for deciding what is
worth retrying: ErrUnretryable, Unretryable, and IsTerminal. It also holds the
vocabulary for reporting a loop that gave up — ErrExhausted, ExhaustedError,
Exhausted, and Attempts — so a caller can tell "the database refused the
connection" from "the database refused the connection five times over four
seconds", which is the difference between a failed request and a request that
spent its latency budget failing. Policies are constructed from retry/config,
which owns Config, the exponential-backoff implementation with optional jitter,
and the DelayFor schedule that callers who cannot retry by sleeping use to
compute their own wake-up times.

The split runs that way — rather than the config subpackage wrapping a root
constructor — because everything consuming a Config has to sit on the same side
of the import edge as Config itself, and config subpackages in this module
import their root, never the reverse.
*/
package retry
