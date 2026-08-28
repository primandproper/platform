package identity

// What a page of rows is, once the generated package has answered.
//
// This file used to be the scan side of the column lists in
// identity/internal/queries: an "aux" type per entity holding the nullable
// columns, a targets method producing the destinations for one Scan, and an
// apply method converting them onto the value. Every statement that needed one
// is generated now — the batch read by id was the last of them — so what is
// left is the shape a page arrives in rather than the pairing of a projection
// with a list of scan targets. A row becomes a domain value in
// identity/rows.go, where a renamed column is a compile error rather than a
// scan that lands one column to the left.

// pageRow is one row of a rendered list query: the value, and the two counts
// the statement carries beside it.
//
// The counts ride on the rows rather than arriving from a second query, which
// is what makes a page and the number describing it come from one snapshot of
// the table. It also means a page with no rows carries no counts — see
// filtering.Drain, which is what reports that as unknown rather than as zero.
type pageRow[T any] struct {
	value    *T
	filtered int64
	total    int64
}

// pageCounts reads the counts off a row, for filtering.Drain.
func pageCounts[T any](row pageRow[T]) (filtered, total int64) {
	return row.filtered, row.total
}

// countOf narrows a COUNT that came back from its own statement to the unsigned
// pair filtering.Pagination reports.
//
// The rendered lists carry their counts on the rows and go through
// filtering.Drain, which narrows them itself. The prefix search's count is a
// second statement, so the narrowing is here — and it is not a guard against
// the database: a COUNT cannot be negative, and what it stops is the conversion
// turning one into a number larger than the table could hold.
func countOf(count int64) uint64 {
	if count < 0 {
		return 0
	}

	return uint64(count)
}

// pageValue reads the value off a row, for filtering.Drain. The value is
// returned as it stands rather than copied, so whatever a caller did to the
// slice of pointers before draining — attaching roles, redacting — is what the
// page carries.
func pageValue[T any](row pageRow[T]) *T { return row.value }

// pageValues collects a page's values, for the passes a caller makes over them
// before draining.
func pageValues[T any](rows []pageRow[T]) []*T {
	values := make([]*T, 0, len(rows))
	for _, row := range rows {
		values = append(values, row.value)
	}

	return values
}
