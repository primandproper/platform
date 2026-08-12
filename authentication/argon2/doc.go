/*
Package argon2 is the argon2id authentication.Authenticator: the password
hasher this module recommends, and the only implementation of that interface it
ships.

# The parameters are fixed, and that is the point

Cost is not configurable. Every hash this package mints uses 64 MiB of memory,
one iteration, a 16-byte salt, and a 32-byte key, with the parallelism degree
taken from runtime.NumCPU and clamped to the range [2, 255]. The clamp is not
decorative: narrowing NumCPU to the uint8 the parameter is declared as
overflows to 0 on a host with 256 CPUs, and x/crypto's argon2 panics on a
parallelism degree of 0.

Leaving cost out of the config is a deliberate trade. A tuning knob for
password hashing is a knob that gets turned down — under load, during an
incident, by whoever is looking at a latency graph — and the deployment that
quietly runs at a tenth of the intended cost is indistinguishable from the one
that does not until its hashes are being cracked. A caller that genuinely needs
different parameters wants its own Authenticator, not a lower number here.

The 64 MiB is per concurrent hash, not per process. A burst of sign-ins is a
burst of 64 MiB allocations, and sizing a service's memory limit without
accounting for its peak concurrent logins is how this shows up as an OOM kill
rather than as latency.

# Raising the cost later is safe

The encoded hash carries the parameters it was created with, and verification
reads them back out of the stored string rather than from the values above. So
raising Memory or Iterations in a later release keeps every existing password
verifiable — old hashes verify at their old cost, new ones are minted at the
new cost — and the parallelism degree differing between hosts, which it will
whenever the fleet is not homogeneous, is harmless for the same reason.

Nothing here re-hashes on verify, so an old hash stays at its old cost until
the password is changed. A deployment that wants the fleet migrated needs to
re-hash on successful sign-in itself.

# Errors

PasswordMatches distinguishes a wrong password from a broken one: a non-match
is (false, nil), and err is populated only when the stored hash is malformed or
the comparison could not be performed. A caller that treats any error as a
failed sign-in is correct; one that treats a failed sign-in as an error is not.
*/
package argon2
