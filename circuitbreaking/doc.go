/*
Package circuitbreaking implements the circuit breaker pattern for managing service availability and preventing cascading failures.

The breaker itself lives in circuitbreaking/config, which builds one from a
Config and wires its state changes to the pillars: a trip and a reset are each
logged and counted, while the individual failures behind them are only counted —
a dependency that is down produces one failure per attempt for as long as it
stays down, and the trip is the event those are evidence for.

Every measurement carries the breaker's name as an attribute rather than in the
instrument name, so one breaker's trips can be read on their own and every
breaker's trips can be read together. Name your breakers: an unnamed one is
given a numbered placeholder, which is enough to keep two of them out of the
same series and no help at all to whoever reads the series later.
*/
package circuitbreaking
