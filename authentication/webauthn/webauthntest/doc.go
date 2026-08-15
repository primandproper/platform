// Package webauthntest holds the behavior every webauthn.SessionStore owes its
// callers, written once and run against each implementation.
//
// The store is the piece of a WebAuthn deployment most likely to be written
// again — a consumer with neither a SQL database nor a cache.Cache has to write
// one — and it is the piece whose failures are the least visible. A store that
// hands the same challenge to two callers, or that keeps handing one out after
// its deadline, produces a login that works, which is what makes the gap
// something a suite has to catch rather than a user.
//
// Expiry is the reason it exists at all. The two implementations here express
// it in completely different terms — an expires_at column compared against a
// clock, and a cache entry the provider drops on its own — and the only thing
// that keeps those two answering a caller the same way is a suite that asks
// both.
//
// # Using it
//
//	func TestSessionStore_Conformance(t *testing.T) {
//		t.Parallel()
//
//		webauthntest.Run(t, func(tb testing.TB) webauthn.SessionStore {
//			store, err := NewSessionStore(&Config{}, newTestClient(tb))
//			must.NoError(tb, err)
//
//			return store
//		})
//	}
//
// Each implementation keeps its own test file for what is genuinely its own —
// the sweeper and the dialect rendering for the database store, the cache
// provider's failures for the cache store.
//
// # Declaring a deviation
//
// The Options are how an implementation says where it stops honoring the full
// contract, and every one of them removes cases. They are deliberately shaped
// so that silence means the whole contract: a store that needs one and does not
// declare it fails, rather than skipping something nobody notices. A declared
// deviation still runs as a skipped subtest naming the reason, so `go test -v`
// shows what was not proven instead of hiding it.
//
// # Real clocks, generous windows
//
// The suite uses the wall clock. No implementation's notion of now can be
// replaced from out here — the database store's is a clock it was constructed
// with, the cache store's belongs to the cache provider — so the expiry case
// saves with a short TTL and then waits several times that before asserting the
// state has lapsed. The window is picked so that a loaded CI host cannot land
// inside it, not so that the suite is fast.
package webauthntest
