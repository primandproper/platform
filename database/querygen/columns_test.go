package querygen

import (
	"slices"
	"testing"

	"github.com/shoenig/test"
)

func TestForInsert(T *testing.T) {
	T.Parallel()

	T.Run("drops the database-owned columns and keeps order", func(t *testing.T) {
		t.Parallel()

		got := ForInsert([]string{
			IDColumn,
			"name",
			LastIndexedAtColumn,
			"notes",
			CreatedAtColumn,
			LastUpdatedAtColumn,
			ArchivedAtColumn,
		})

		test.Eq(t, []string{IDColumn, "name", "notes"}, got)
	})

	T.Run("drops the caller's exceptions too", func(t *testing.T) {
		t.Parallel()

		got := ForInsert([]string{IDColumn, "name", "computed"}, "computed")

		test.Eq(t, []string{IDColumn, "name"}, got)
	})

	T.Run("keeps the id, which the application supplies", func(t *testing.T) {
		t.Parallel()

		test.Eq(t, []string{IDColumn}, ForInsert([]string{IDColumn, CreatedAtColumn}))
	})

	T.Run("leaves the caller's slice alone", func(t *testing.T) {
		t.Parallel()

		columns := []string{IDColumn, "name", CreatedAtColumn}
		ForInsert(columns, "name")

		test.Eq(t, []string{IDColumn, "name", CreatedAtColumn}, columns)
	})

	T.Run("exceptions do not accumulate across calls", func(t *testing.T) {
		t.Parallel()

		// The database-owned list is package state, so an implementation that
		// appended the caller's exceptions onto it rather than onto a copy would
		// have every table after the first one silently missing another table's
		// columns.
		columns := []string{IDColumn, "name", "notes"}

		ForInsert(columns, "notes")

		test.Eq(t, []string{IDColumn, "name", "notes"}, ForInsert(columns))
		test.Eq(t, databaseOwnedColumns, []string{
			ArchivedAtColumn,
			CreatedAtColumn,
			LastUpdatedAtColumn,
			LastIndexedAtColumn,
		})
	})
}

func TestForUpdate(T *testing.T) {
	T.Parallel()

	T.Run("drops the id along with the database-owned columns", func(t *testing.T) {
		t.Parallel()

		got := ForUpdate([]string{IDColumn, "name", CreatedAtColumn, ArchivedAtColumn})

		test.Eq(t, []string{"name"}, got)
	})

	T.Run("drops the caller's exceptions too", func(t *testing.T) {
		t.Parallel()

		got := ForUpdate([]string{IDColumn, "name", BelongsToAccountColumn}, BelongsToAccountColumn)

		test.Eq(t, []string{"name"}, got)
	})

	T.Run("is empty for a table with nothing mutable", func(t *testing.T) {
		t.Parallel()

		test.SliceEmpty(t, ForUpdate([]string{IDColumn, CreatedAtColumn, ArchivedAtColumn}))
	})
}

func TestQualify(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, "things.id", Qualify("things", IDColumn))
	})
}

func TestQualifyAll(T *testing.T) {
	T.Parallel()

	T.Run("qualifies every column in order", func(t *testing.T) {
		t.Parallel()

		got := QualifyAll("things", []string{IDColumn, "name"})

		test.Eq(t, []string{"things.id", "things.name"}, got)
	})

	T.Run("empty in, empty out", func(t *testing.T) {
		t.Parallel()

		test.SliceEmpty(t, QualifyAll("things", nil))
	})
}

func TestWithout(T *testing.T) {
	T.Parallel()

	T.Run("removes every occurrence and keeps order", func(t *testing.T) {
		t.Parallel()

		test.Eq(t, []string{"a", "c"}, without([]string{"a", "b", "c", "b"}, "b"))
	})
}

// columnsFor is the conventional column set used across the tests, in the order
// a generated SELECT would list it.
func columnsFor(extra ...string) []string {
	return slices.Concat(
		[]string{IDColumn, "name"},
		extra,
		[]string{CreatedAtColumn, LastUpdatedAtColumn, ArchivedAtColumn},
	)
}
