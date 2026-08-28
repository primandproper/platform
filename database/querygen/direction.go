package querygen

import (
	"github.com/primandproper/platform-go/v13/filtering"
)

// Direction is which way a keyset walk pages: oldest first, or newest first.
//
// It is a parameter rather than a value a statement binds, and that is the
// whole shape of this feature. A direction decides which way the ORDER BY runs
// and which way the cursor comparison points, and both of those are statement
// text — there is no expression that takes a bound argument and orders by it in
// either direction on all three of these servers, and assembling one per
// request is the dynamic SQL this package exists to replace. So a paged list is
// two statements, written down in both directions, and what a store does with
// filtering.QueryFilter.SortBy is choose between them.
//
// The zero value is [Ascending], which is what filtering.DefaultQueryFilter
// asks for and what every list in this module answered before the descending
// half existed.
type Direction int

const (
	// Ascending pages oldest first: rows after the cursor, in increasing order.
	Ascending Direction = iota
	// Descending pages newest first: rows before the cursor, in decreasing
	// order.
	Descending
)

// String names the direction, for error messages and test failures.
func (d Direction) String() string {
	if d == Descending {
		return "descending"
	}

	return "ascending"
}

// keyword is how an ORDER BY spells the direction. It is written out on both
// arms rather than being left to the server's default, so that reading the
// emitted statement answers the question a reader of it has.
func (d Direction) keyword() string {
	if d == Descending {
		return "DESC"
	}

	return "ASC"
}

// DirectionOf returns the direction filter asks for.
//
// It is here rather than in filtering for the reason [BindFilter] is: filtering
// owns the field and its vocabulary, and the translation into what a statement
// is shaped like belongs to whatever renders the statement. A nil filter is
// [Ascending], as an absent or unrecognized SortBy is — see
// filtering.QueryFilter.SortsDescending, which is the one reading of that field
// there is.
func DirectionOf(filter *filtering.QueryFilter) Direction {
	if filter.SortsDescending() {
		return Descending
	}

	return Ascending
}

// DescendingSuffix is what a paged list's descending half is named with: the
// ascending statement's name and this.
//
// It is derived rather than taken as a second argument because a query name is
// a generated Go method name, and two names for one list is two things a
// consumer can get inconsistent — a corpus whose descending statements are
// named by hand is a corpus where one of them is called ListUsersDesc. Derived,
// the pair is one decision, and a caller reading ListUsers in a store knows
// what the other one is called without looking.
const DescendingSuffix = "Descending"

// DescendingName is the name the descending half of a paged list is emitted
// under.
func DescendingName(name string) string {
	return name + DescendingSuffix
}
