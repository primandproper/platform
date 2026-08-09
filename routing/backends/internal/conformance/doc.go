// Package conformance holds the tests that pin every routing.Backend to one
// answer, rather than to whatever its underlying library happens to do.
//
// The per-backend test files each prove their own backend works. Nothing proved
// that the four agree, and they did not: a path value carrying a reserved
// character used to round-trip on exactly one of them, silently, so the same
// service answered correctly, corrupted the value, or 404'd depending on which
// router a deployment had configured. A behavior a caller is entitled to expect
// from routing.Backend is asserted here, against every implementation at once,
// so a divergence is a failing test rather than a portability trap.
//
// It has no non-test code. It lives under internal/ because it is a statement
// about this repository's backends, not surface anything outside can use.
package conformance
