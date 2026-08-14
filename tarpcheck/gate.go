// Package tarpcheck is a throwaway, reverted two commits from now. It makes the
// branch touch Go source so the workflow runs, and pairs with a threshold one
// point above the current score to prove the gate fails rather than assuming it.
package tarpcheck

// Untested has no test that names it.
func Untested(n int) int { return n + 1 }
