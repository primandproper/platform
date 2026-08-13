// Package distributedlocktest holds the behavior every distributedlock.Locker
// and distributedlock.ScopedLocker owes its callers, written once and run
// against each implementation.
//
// Each provider used to carry its own copy of these cases, and the copies had
// already drifted apart. Only postgres asserted that two separate Lockers
// contend over one key, and that a Refresh after a Release is refused. Only
// memory asserted that Acquire rejects a negative TTL, or raced goroutines for
// one key — and it counted the winners without checking that the losers were
// told ErrLockNotAcquired rather than something else. Postgres reached its
// expiry cases by writing to the handle's expiry field behind a sqlmock, so
// nothing proved the real backend behaves that way. None of the three asserted
// that two distinct keys do not contend. On the scoped side, neither
// implementation asserted an empty key, and each asserted the panic case on
// the method the other did not.
//
// Nothing was wrong with the providers — the gaps were in what each file
// happened to think of, and a gap in one file is invisible from the others. A
// behavior a caller is entitled to expect from the interface belongs here,
// where an implementation that disagrees fails rather than passes quietly.
//
// # Why it is exported
//
// The suite is not internal, unlike routing/backends/internal/conformance,
// because the in-repo test doubles are the strongest reason to have it.
// distributedlock/memory stands in for a real lock across this repository and
// in consumers, and a double is a claim about the real thing: every suite that
// schedules against it inherits whatever it gets wrong. Running the same cases
// against the double and against redis and postgres is what keeps that claim
// honest. A consumer writing a fifth implementation runs Run against it and
// finds out whether it belongs, rather than discovering in production that the
// interface only ever said what compiles.
//
// # Using it
//
//	func TestLocker_Conformance(t *testing.T) {
//		t.Parallel()
//
//		distributedlocktest.Run(t, func(tb testing.TB) distributedlock.Locker {
//			l, err := NewLocker()
//			must.NoError(tb, err)
//			tb.Cleanup(func() { must.NoError(tb, l.Close()) })
//
//			return l
//		}, distributedlocktest.WithInstanceLocalStore())
//	}
//
// Each implementation keeps its own test file for what is genuinely its own —
// pool saturation and advisory-lock ids for postgres, a forged ownership token
// for redis, container wiring for both.
//
// # Declaring a deviation
//
// The Options are how an implementation says where it stops honoring the full
// contract, and every one of them removes cases. They are deliberately shaped
// so that silence means the whole contract: a provider that needs one and does
// not declare it fails, rather than skipping something nobody notices. A
// declared deviation still runs as a skipped subtest naming the reason, so
// `go test -v` shows what was not proven instead of hiding it.
//
// # Real clocks, generous windows
//
// The suite uses the wall clock. No provider's notion of now can be replaced
// from out here — postgres' is the server's, redis' is the server's — so the
// expiry cases acquire with a short TTL and then wait several times that
// before asserting the lock has lapsed. The windows are picked so that a
// loaded CI host cannot land between them, not so that the suite is fast.
//
// # What it deliberately does not pin
//
// Close. The interface promises that outstanding handles "may become invalid"
// after it, which is not a behavior a caller can rely on and not one a suite
// can assert: postgres releases every outstanding advisory lock, redis closes
// the client and leaves the keys to expire on their own TTL, memory drops the
// map. Both are conformant with what the interface says, so each provider
// tests its own answer in its own file, and a caller that needs one of those
// answers is choosing a provider rather than an interface.
//
// # What it is not run against
//
// distributedlock/noop, which arbitrates nothing by design: its Acquire always
// succeeds, its Release always reports success, and its TTL is never enforced.
// It is not an implementation that would fail this suite so much as one the
// suite has nothing to say about — mutual exclusion is the whole contract, and
// noop's own doc is explicit that it provides none of it. Running it here with
// enough deviations declared to pass would turn the suite into a shape check.
package distributedlocktest
