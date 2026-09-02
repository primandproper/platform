package waitlists

import (
	"testing"
	"time"

	platformerrors "github.com/primandproper/platform-go/v14/errors"
	"github.com/primandproper/platform-go/v14/filtering"
	"github.com/primandproper/platform-go/v14/pointer"
	"github.com/primandproper/platform-go/v14/tenancy"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// runListSuite is every assertion about the catalog half of the store.
func runListSuite(t *testing.T, env *storeEnv) {
	t.Helper()

	t.Run("CreateList", func(T *testing.T) {
		T.Run("stores the list and stamps the database's creation time", func(t *testing.T) {
			t.Parallel()

			store := env.newStore(t)

			created := mustCreateList(t, store, testScope, openList("Launch"))

			test.NotEqOp(t, "", created.ID)
			test.EqOp(t, testScope, created.Scope)
			test.EqOp(t, "Launch", created.Name)
			test.False(t, created.CreatedAt.IsZero())
			test.Nil(t, created.LastUpdatedAt)
			test.Nil(t, created.ArchivedAt)

			read, err := store.GetList(t.Context(), testScope, created.ID)
			must.NoError(t, err)
			test.EqOp(t, created.ID, read.ID)
			test.EqOp(t, created.ClosesAt.UTC(), read.ClosesAt)
		})

		T.Run("refuses a list with nothing to identify or close it", func(t *testing.T) {
			t.Parallel()

			store := env.newStore(t)

			_, err := store.CreateList(t.Context(), testScope, nil)
			test.ErrorIs(t, err, ErrNilList)

			_, err = store.CreateList(t.Context(), testScope, &List{ClosesAt: testNow})
			test.ErrorIs(t, err, ErrEmptyListName)

			// There is no default for the closing time — see ErrEmptyClosesAt.
			_, err = store.CreateList(t.Context(), testScope, &List{Name: "Launch"})
			test.ErrorIs(t, err, ErrEmptyClosesAt)
		})

		T.Run("refuses an unset scope", func(t *testing.T) {
			t.Parallel()

			store := env.newStore(t)

			_, err := store.CreateList(t.Context(), tenancy.Scope{}, openList("Launch"))
			test.Error(t, err)
		})
	})

	t.Run("GetList", func(T *testing.T) {
		T.Run("does not cross scopes", func(t *testing.T) {
			t.Parallel()

			store := env.newStore(t)

			created := mustCreateList(t, store, testScope, openList("Launch"))

			_, err := store.GetList(t.Context(), otherScope, created.ID)
			test.ErrorIs(t, err, ErrListNotFound)
		})

		T.Run("reports a missing list rather than an empty one", func(t *testing.T) {
			t.Parallel()

			store := env.newStore(t)

			_, err := store.GetList(t.Context(), testScope, "nope")
			test.ErrorIs(t, err, ErrListNotFound)

			_, err = store.GetList(t.Context(), testScope, "")
			test.ErrorIs(t, err, platformerrors.ErrInvalidIDProvided)
		})
	})

	t.Run("ListLists", func(T *testing.T) {
		T.Run("pages the scope's catalog in both directions", func(t *testing.T) {
			t.Parallel()

			store := env.newStore(t)

			first := mustCreateList(t, store, testScope, openList("first"))
			second := mustCreateList(t, store, testScope, openList("second"))
			mustCreateList(t, store, otherScope, openList("theirs"))

			page, err := store.ListLists(t.Context(), testScope, nil)
			must.NoError(t, err)
			must.SliceLen(t, 2, page.Data)
			test.EqOp(t, first.ID, page.Data[0].ID)
			test.EqOp(t, second.ID, page.Data[1].ID)

			descending := filtering.DefaultQueryFilter()
			descending.SortBy = filtering.SortDescending

			page, err = store.ListLists(t.Context(), testScope, descending)
			must.NoError(t, err)
			must.SliceLen(t, 2, page.Data)
			test.EqOp(t, second.ID, page.Data[0].ID)
		})

		T.Run("leaves archived lists out unless asked", func(t *testing.T) {
			t.Parallel()

			store := env.newStore(t)

			created := mustCreateList(t, store, testScope, openList("Launch"))
			must.NoError(t, store.ArchiveList(t.Context(), testScope, created.ID))

			page, err := store.ListLists(t.Context(), testScope, nil)
			must.NoError(t, err)
			test.SliceEmpty(t, page.Data)

			withArchived := filtering.DefaultQueryFilter()
			withArchived.IncludeArchived = pointer.To(true)

			page, err = store.ListLists(t.Context(), testScope, withArchived)
			must.NoError(t, err)
			must.SliceLen(t, 1, page.Data)
			test.NotNil(t, page.Data[0].ArchivedAt)
		})
	})

	t.Run("ListOpenLists", func(T *testing.T) {
		T.Run("answers on the store's clock, not the server's", func(t *testing.T) {
			t.Parallel()

			c := newStubClock()
			store := env.newStore(t, WithClock(c))

			open := mustCreateList(t, store, testScope, openList("open"))
			mustCreateList(t, store, testScope, closedList("closed"))

			page, err := store.ListOpenLists(t.Context(), testScope, nil)
			must.NoError(t, err)
			must.SliceLen(t, 1, page.Data)
			test.EqOp(t, open.ID, page.Data[0].ID)

			// The whole reason the horizon is bound rather than read off
			// CURRENT_TIMESTAMP: moving the store's clock past the closing time
			// takes the list off this page, and List.OpenAt agrees.
			c.advance(31 * 24 * time.Hour)

			page, err = store.ListOpenLists(t.Context(), testScope, nil)
			must.NoError(t, err)
			test.SliceEmpty(t, page.Data)

			read, err := store.GetList(t.Context(), testScope, open.ID)
			must.NoError(t, err)
			test.False(t, read.OpenAt(c.Now()))
		})

		T.Run("pages descending too", func(t *testing.T) {
			t.Parallel()

			store := env.newStore(t)

			mustCreateList(t, store, testScope, openList("first"))
			second := mustCreateList(t, store, testScope, openList("second"))

			descending := filtering.DefaultQueryFilter()
			descending.SortBy = filtering.SortDescending

			page, err := store.ListOpenLists(t.Context(), testScope, descending)
			must.NoError(t, err)
			must.SliceLen(t, 2, page.Data)
			test.EqOp(t, second.ID, page.Data[0].ID)
		})

		T.Run("an archived list is closed whatever its closing time says", func(t *testing.T) {
			t.Parallel()

			store := env.newStore(t)

			created := mustCreateList(t, store, testScope, openList("Launch"))
			must.NoError(t, store.ArchiveList(t.Context(), testScope, created.ID))

			page, err := store.ListOpenLists(t.Context(), testScope, nil)
			must.NoError(t, err)
			test.SliceEmpty(t, page.Data)
		})
	})

	t.Run("UpdateList", func(T *testing.T) {
		T.Run("rewrites the name, description and closing time", func(t *testing.T) {
			t.Parallel()

			store := env.newStore(t)

			created := mustCreateList(t, store, testScope, openList("Launch"))

			created.Name = "Beta"
			created.Description = "now with fewer bugs"
			created.ClosesAt = testNow.Add(90 * 24 * time.Hour)

			must.NoError(t, store.UpdateList(t.Context(), testScope, created))

			read, err := store.GetList(t.Context(), testScope, created.ID)
			must.NoError(t, err)
			test.EqOp(t, "Beta", read.Name)
			test.EqOp(t, "now with fewer bugs", read.Description)
			test.EqOp(t, created.ClosesAt.UTC(), read.ClosesAt)
			test.NotNil(t, read.LastUpdatedAt)
		})

		T.Run("brings a closed list back by moving its horizon", func(t *testing.T) {
			t.Parallel()

			store := env.newStore(t)

			created := mustCreateList(t, store, testScope, closedList("Launch"))

			created.ClosesAt = testNow.Add(time.Hour)
			must.NoError(t, store.UpdateList(t.Context(), testScope, created))

			page, err := store.ListOpenLists(t.Context(), testScope, nil)
			must.NoError(t, err)
			must.SliceLen(t, 1, page.Data)
		})

		T.Run("refuses what it cannot address", func(t *testing.T) {
			t.Parallel()

			store := env.newStore(t)

			created := mustCreateList(t, store, testScope, openList("Launch"))

			test.ErrorIs(t, store.UpdateList(t.Context(), testScope, nil), ErrNilList)
			test.ErrorIs(t, store.UpdateList(t.Context(), testScope, &List{ClosesAt: testNow, Name: "x"}),
				platformerrors.ErrInvalidIDProvided)

			// Another scope's list is not found rather than rewritten, which is
			// the whole of what the scope predicate on the write is for.
			test.ErrorIs(t, store.UpdateList(t.Context(), otherScope, created), ErrListNotFound)

			must.NoError(t, store.ArchiveList(t.Context(), testScope, created.ID))
			test.ErrorIs(t, store.UpdateList(t.Context(), testScope, created), ErrListNotFound)
		})
	})

	t.Run("ArchiveList", func(T *testing.T) {
		T.Run("is idempotent and scoped", func(t *testing.T) {
			t.Parallel()

			store := env.newStore(t)

			created := mustCreateList(t, store, testScope, openList("Launch"))

			test.ErrorIs(t, store.ArchiveList(t.Context(), otherScope, created.ID), ErrListNotFound)

			must.NoError(t, store.ArchiveList(t.Context(), testScope, created.ID))

			// A second archive reports the row was not there to archive rather
			// than restamping the moment it was retired.
			test.ErrorIs(t, store.ArchiveList(t.Context(), testScope, created.ID), ErrListNotFound)
		})

		T.Run("leaves the signups against it readable", func(t *testing.T) {
			t.Parallel()

			store := env.newStore(t)

			list := mustCreateList(t, store, testScope, openList("Launch"))
			signup := mustJoin(t, store, testScope, list.ID, &Signup{Contact: "ada@example.com"})

			must.NoError(t, store.ArchiveList(t.Context(), testScope, list.ID))

			// Archiving is not erasure: the queue is still there to be worked
			// through, and only the list has left the catalog.
			read, err := store.GetSignup(t.Context(), testScope, list.ID, signup.ID)
			must.NoError(t, err)
			test.EqOp(t, signup.ID, read.ID)
		})
	})
}
