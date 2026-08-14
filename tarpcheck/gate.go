// Package tarpcheck is a throwaway, reverted in the next commit. It exists to
// make this branch touch Go source so the tarp workflow runs on its own pull
// request, and to put one function in front of it that nothing tests.
package tarpcheck

// Untested has no test that names it.
func Untested(n int) int { return n + 1 }
