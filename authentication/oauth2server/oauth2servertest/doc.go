// Package oauth2servertest holds the behavior every oauth2server.Store owes its
// callers, written once and run against each implementation.
//
// A store whose entire job is one-time use and expiry needs a single suite
// proving its implementations agree, because the cases that separate them are
// exactly the ones a map gets for free and a table does not. Consuming an
// authorization code is a read and a write in the map store and a guarded
// UPDATE in the database one; nothing but a shared suite says those two mean
// the same thing. The two cases that matter most — a code redeemed twice
// concurrently, and a record that expires between the read and the write — are
// unremarkable under a mutex and are where a hand-written SQL store goes wrong.
//
// # Using it
//
//	func TestStore_Conformance(t *testing.T) {
//		t.Parallel()
//
//		oauth2servertest.Run(t, func(tb testing.TB) oauth2server.Store {
//			s := memory.NewStore()
//			tb.Cleanup(func() { must.NoError(tb, s.Close()) })
//
//			return s
//		}, oauth2servertest.WithInstanceLocalState())
//
// Each implementation keeps its own test file for what is genuinely its own —
// DDL rendering and dialect placeholders for the database store, container
// wiring, whatever its options do.
//
// # Declaring a deviation
//
// The Options are how an implementation says where it stops honoring the full
// contract, and each one removes cases. Silence means the whole contract, so a
// store that needs one and does not declare it fails rather than skipping
// something nobody notices. A declared deviation still runs as a skipped
// subtest naming the reason, so `go test -v` shows what was not proven.
//
// # Expiry is data, not a timer
//
// Every record here carries its own ExpiresAt, so the suite reaches expired
// state by writing a deadline in the past rather than by sleeping. That makes
// the expiry cases deterministic on a loaded host, and it is only possible
// because the interface puts the deadline in the record instead of deriving it
// from a TTL the backend owns.
//
// The one thing it cannot reach that way is the store's own notion of now,
// which for the database store is the server's. The suite therefore asserts
// relative facts — a deadline one hour ago is past, one hour ahead is not —
// with margins wide enough that no plausible clock skew between a test process
// and its database lands inside them.
package oauth2servertest
