package querygen

import (
	"testing"

	"github.com/primandproper/platform-go/v13/filtering"

	"github.com/shoenig/test"
)

// TestDirectionOf pins the translation this package makes between filtering's
// vocabulary and its own. It is the whole seam: a store reads SortBy through
// filtering and picks a statement through here, so the two disagreeing would be
// a filter that asked for one page and a query that answered the other.
func TestDirectionOf(T *testing.T) {
	T.Parallel()

	T.Run("a descending filter picks the descending statement", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, Descending, DirectionOf(&filtering.QueryFilter{SortBy: filtering.SortDescending}))
	})

	T.Run("everything else picks the ascending one", func(t *testing.T) {
		t.Parallel()

		// A nil filter is the default filter everywhere else in this module,
		// and the default filter is ascending.
		test.EqOp(t, Ascending, DirectionOf(nil))
		test.EqOp(t, Ascending, DirectionOf(&filtering.QueryFilter{}))
		test.EqOp(t, Ascending, DirectionOf(filtering.DefaultQueryFilter()))
		test.EqOp(t, Ascending, DirectionOf(&filtering.QueryFilter{SortBy: filtering.SortAscending}))
	})

	T.Run("the zero Direction is the ascending one", func(t *testing.T) {
		t.Parallel()

		// Which is what makes every statement emitted before the descending
		// half existed the same statement it was.
		var zero Direction

		test.EqOp(t, Ascending, zero)
	})
}

func TestDirection_keyword(t *testing.T) {
	t.Parallel()

	// Spelled either way rather than left to the server's default, so reading
	// the statement answers the question its reader has.
	test.EqOp(t, "ASC", Ascending.keyword())
	test.EqOp(t, "DESC", Descending.keyword())
}

func TestDirection_String(t *testing.T) {
	t.Parallel()

	test.EqOp(t, "ascending", Ascending.String())
	test.EqOp(t, "descending", Descending.String())
}

func TestDescendingName(t *testing.T) {
	t.Parallel()

	// Derived rather than taken as a second argument: a query name is a
	// generated Go method name, and two names for one list is two things that
	// can come to disagree.
	test.EqOp(t, "ListUsersDescending", DescendingName("ListUsers"))
	test.EqOp(t, "ListInvitationsByFromUserDescending", DescendingName("ListInvitationsByFromUser"))
}
