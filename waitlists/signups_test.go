package waitlists

import (
	"testing"
	"time"

	platformerrors "github.com/primandproper/platform-go/v14/errors"
	"github.com/primandproper/platform-go/v14/filtering"
	"github.com/primandproper/platform-go/v14/pointer"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// runSignupSuite is every assertion about joining a list, reading the queue, and
// moving through it.
func runSignupSuite(t *testing.T, env *storeEnv) {
	t.Helper()

	t.Run("Join", func(T *testing.T) {
		T.Run("stores the contact as given and the digest of its normalization", func(t *testing.T) {
			t.Parallel()

			store := env.newStore(t)
			list := mustCreateList(t, store, testScope, openList("Launch"))

			joined := mustJoin(t, store, testScope, list.ID, &Signup{
				Contact: "Ada@Example.com",
				Notes:   "met at the conference",
				Subject: testSubject,
			})

			test.NotEqOp(t, "", joined.ID)
			test.EqOp(t, "Ada@Example.com", joined.Contact)
			test.EqOp(t, store.Digest("ada@example.com"), joined.ContactDigest)
			test.EqOp(t, StatusWaiting, joined.Status)
			test.Nil(t, joined.StatusChangedAt)
			test.False(t, joined.CreatedAt.IsZero())
		})

		T.Run("the status and the stamps are the store's, not the caller's", func(t *testing.T) {
			t.Parallel()

			store := env.newStore(t)
			list := mustCreateList(t, store, testScope, openList("Launch"))

			// A caller describing a signup that has already happened would
			// otherwise put somebody straight into a status the lifecycle
			// guards exist to move them through.
			joined := mustJoin(t, store, testScope, list.ID, &Signup{
				Contact:         "ada@example.com",
				Status:          StatusConverted,
				StatusChangedAt: pointer.To(testNow),
				ArchivedAt:      pointer.To(testNow),
			})

			test.EqOp(t, StatusWaiting, joined.Status)
			test.Nil(t, joined.StatusChangedAt)
			test.Nil(t, joined.ArchivedAt)
		})

		T.Run("refuses a closed list, an archived one, and a missing one", func(t *testing.T) {
			t.Parallel()

			store := env.newStore(t)

			closed := mustCreateList(t, store, testScope, closedList("closed"))
			archived := mustCreateList(t, store, testScope, openList("archived"))
			must.NoError(t, store.ArchiveList(t.Context(), testScope, archived.ID))

			_, err := store.Join(t.Context(), testScope, closed.ID, &Signup{Contact: "ada@example.com"})
			test.ErrorIs(t, err, ErrListClosed)

			// An archived list is not found rather than closed: the read that
			// resolves it excludes archived rows, and a caller holding the id
			// of a retired list is holding a broken link.
			_, err = store.Join(t.Context(), testScope, archived.ID, &Signup{Contact: "ada@example.com"})
			test.ErrorIs(t, err, ErrListNotFound)

			_, err = store.Join(t.Context(), testScope, "nope", &Signup{Contact: "ada@example.com"})
			test.ErrorIs(t, err, ErrListNotFound)
		})

		T.Run("refuses a signup naming nothing to write to", func(t *testing.T) {
			t.Parallel()

			store := env.newStore(t)
			list := mustCreateList(t, store, testScope, openList("Launch"))

			_, err := store.Join(t.Context(), testScope, list.ID, nil)
			test.ErrorIs(t, err, ErrNilSignup)

			_, err = store.Join(t.Context(), testScope, list.ID, &Signup{Contact: "   "})
			test.ErrorIs(t, err, ErrEmptyContact)
		})

		T.Run("refuses half a subject", func(t *testing.T) {
			t.Parallel()

			store := env.newStore(t)
			list := mustCreateList(t, store, testScope, openList("Launch"))

			_, err := store.Join(t.Context(), testScope, list.ID, &Signup{
				Contact: "ada@example.com",
				Subject: Subject{Type: SubjectUser},
			})
			test.ErrorIs(t, err, ErrEmptySubjectID)

			_, err = store.Join(t.Context(), testScope, list.ID, &Signup{
				Contact: "ada@example.com",
				Subject: Subject{ID: "user-1"},
			})
			test.ErrorIs(t, err, ErrEmptySubjectType)
		})

		T.Run("refuses a contact already on the list, whatever its capitalization", func(t *testing.T) {
			t.Parallel()

			store := env.newStore(t)
			list := mustCreateList(t, store, testScope, openList("Launch"))

			mustJoin(t, store, testScope, list.ID, &Signup{Contact: "ada@example.com"})

			_, err := store.Join(t.Context(), testScope, list.ID, &Signup{Contact: "  ADA@Example.com "})
			test.ErrorIs(t, err, ErrAlreadySignedUp)
		})

		T.Run("refuses a contact whose signup was archived", func(t *testing.T) {
			t.Parallel()

			store := env.newStore(t)
			list := mustCreateList(t, store, testScope, openList("Launch"))

			signup := mustJoin(t, store, testScope, list.ID, &Signup{Contact: "ada@example.com"})
			must.NoError(t, store.ArchiveSignup(t.Context(), testScope, list.ID, signup.ID))

			// The uniqueness covers archived rows, so the check that stands in
			// for it has to see them — otherwise this is a driver error naming
			// an index rather than something a caller can show somebody.
			_, err := store.Join(t.Context(), testScope, list.ID, &Signup{Contact: "ada@example.com"})
			test.ErrorIs(t, err, ErrAlreadySignedUp)
		})

		T.Run("one contact may be on two lists and in two scopes", func(t *testing.T) {
			t.Parallel()

			store := env.newStore(t)

			first := mustCreateList(t, store, testScope, openList("first"))
			second := mustCreateList(t, store, testScope, openList("second"))
			theirs := mustCreateList(t, store, otherScope, openList("theirs"))

			mustJoin(t, store, testScope, first.ID, &Signup{Contact: "ada@example.com"})
			mustJoin(t, store, testScope, second.ID, &Signup{Contact: "ada@example.com"})
			mustJoin(t, store, otherScope, theirs.ID, &Signup{Contact: "ada@example.com"})
		})
	})

	t.Run("GetSignup", func(T *testing.T) {
		T.Run("keys on the list as well as the id and the scope", func(t *testing.T) {
			t.Parallel()

			store := env.newStore(t)

			list := mustCreateList(t, store, testScope, openList("Launch"))
			other := mustCreateList(t, store, testScope, openList("other"))
			signup := mustJoin(t, store, testScope, list.ID, &Signup{Contact: "ada@example.com"})

			_, err := store.GetSignup(t.Context(), testScope, other.ID, signup.ID)
			test.ErrorIs(t, err, ErrSignupNotFound)

			_, err = store.GetSignup(t.Context(), otherScope, list.ID, signup.ID)
			test.ErrorIs(t, err, ErrSignupNotFound)

			_, err = store.GetSignup(t.Context(), testScope, list.ID, "")
			test.ErrorIs(t, err, platformerrors.ErrInvalidIDProvided)
		})
	})

	t.Run("GetSignupByContact", func(T *testing.T) {
		T.Run("finds a signup under any capitalization of its address", func(t *testing.T) {
			t.Parallel()

			store := env.newStore(t)

			list := mustCreateList(t, store, testScope, openList("Launch"))
			signup := mustJoin(t, store, testScope, list.ID, &Signup{Contact: "Ada@Example.com"})

			read, err := store.GetSignupByContact(t.Context(), testScope, list.ID, " ada@EXAMPLE.com ")
			must.NoError(t, err)
			test.EqOp(t, signup.ID, read.ID)
		})

		T.Run("reports an archived signup as missing and a withdrawn one as withdrawn", func(t *testing.T) {
			t.Parallel()

			store := env.newStore(t)
			list := mustCreateList(t, store, testScope, openList("Launch"))

			archived := mustJoin(t, store, testScope, list.ID, &Signup{Contact: "archived@example.com"})
			must.NoError(t, store.ArchiveSignup(t.Context(), testScope, list.ID, archived.ID))

			_, err := store.GetSignupByContact(t.Context(), testScope, list.ID, "archived@example.com")
			test.ErrorIs(t, err, ErrSignupNotFound)

			gone := mustJoin(t, store, testScope, list.ID, &Signup{Contact: "gone@example.com"})
			must.NoError(t, store.Withdraw(t.Context(), testScope, list.ID, gone.ID))

			// A withdrawn row is live, which is what lets an unsubscribe page
			// say "you are already off this list" rather than "we have never
			// heard of you". The address it was found by is no longer in it.
			read, err := store.GetSignupByContact(t.Context(), testScope, list.ID, "gone@example.com")
			must.NoError(t, err)
			test.EqOp(t, StatusWithdrawn, read.Status)
			test.EqOp(t, "", read.Contact)
		})

		T.Run("refuses an empty contact rather than answering", func(t *testing.T) {
			t.Parallel()

			store := env.newStore(t)
			list := mustCreateList(t, store, testScope, openList("Launch"))

			_, err := store.GetSignupByContact(t.Context(), testScope, list.ID, "  ")
			test.ErrorIs(t, err, ErrEmptyContact)
		})
	})

	t.Run("ListSignups", func(T *testing.T) {
		T.Run("pages one list's queue in the order people joined", func(t *testing.T) {
			t.Parallel()

			store := env.newStore(t)

			list := mustCreateList(t, store, testScope, openList("Launch"))
			other := mustCreateList(t, store, testScope, openList("other"))

			first := mustJoin(t, store, testScope, list.ID, &Signup{Contact: "first@example.com"})
			second := mustJoin(t, store, testScope, list.ID, &Signup{Contact: "second@example.com"})
			mustJoin(t, store, testScope, other.ID, &Signup{Contact: "elsewhere@example.com"})

			page, err := store.ListSignups(t.Context(), testScope, list.ID, nil)
			must.NoError(t, err)
			must.SliceLen(t, 2, page.Data)
			test.EqOp(t, first.ID, page.Data[0].ID)
			test.EqOp(t, second.ID, page.Data[1].ID)

			descending := filtering.DefaultQueryFilter()
			descending.SortBy = filtering.SortDescending

			page, err = store.ListSignups(t.Context(), testScope, list.ID, descending)
			must.NoError(t, err)
			must.SliceLen(t, 2, page.Data)
			test.EqOp(t, second.ID, page.Data[0].ID)
		})

		T.Run("does not cross scopes", func(t *testing.T) {
			t.Parallel()

			store := env.newStore(t)

			list := mustCreateList(t, store, testScope, openList("Launch"))
			mustJoin(t, store, testScope, list.ID, &Signup{Contact: "ada@example.com"})

			page, err := store.ListSignups(t.Context(), otherScope, list.ID, nil)
			must.NoError(t, err)
			test.SliceEmpty(t, page.Data)
		})
	})

	t.Run("ListSignupsForSubject", func(T *testing.T) {
		T.Run("pages one principal's signups across every list", func(t *testing.T) {
			t.Parallel()

			store := env.newStore(t)

			first := mustCreateList(t, store, testScope, openList("first"))
			second := mustCreateList(t, store, testScope, openList("second"))

			mine := mustJoin(t, store, testScope, first.ID, &Signup{Contact: "ada@example.com", Subject: testSubject})
			alsoMine := mustJoin(t, store, testScope, second.ID, &Signup{Contact: "ada@example.com", Subject: testSubject})
			mustJoin(t, store, testScope, first.ID, &Signup{Contact: "anon@example.com"})

			page, err := store.ListSignupsForSubject(t.Context(), testScope, testSubject, nil)
			must.NoError(t, err)
			must.SliceLen(t, 2, page.Data)
			test.EqOp(t, mine.ID, page.Data[0].ID)
			test.EqOp(t, alsoMine.ID, page.Data[1].ID)

			descending := filtering.DefaultQueryFilter()
			descending.SortBy = filtering.SortDescending

			page, err = store.ListSignupsForSubject(t.Context(), testScope, testSubject, descending)
			must.NoError(t, err)
			must.SliceLen(t, 2, page.Data)
			test.EqOp(t, alsoMine.ID, page.Data[0].ID)
		})

		T.Run("refuses the anonymous subject rather than paging everybody", func(t *testing.T) {
			t.Parallel()

			store := env.newStore(t)

			list := mustCreateList(t, store, testScope, openList("Launch"))
			mustJoin(t, store, testScope, list.ID, &Signup{Contact: "anon@example.com"})

			// Both columns default to the empty string, so a read bound to
			// nobody would page every signup nobody claimed — the widest
			// possible answer to a question about one person.
			_, err := store.ListSignupsForSubject(t.Context(), testScope, Subject{}, nil)
			test.ErrorIs(t, err, ErrEmptySubjectType)

			_, err = store.ListSignupsForSubject(t.Context(), testScope, Subject{Type: SubjectUser}, nil)
			test.ErrorIs(t, err, ErrEmptySubjectID)
		})
	})

	t.Run("UpdateSignupNotes", func(T *testing.T) {
		T.Run("moves nobody", func(t *testing.T) {
			t.Parallel()

			store := env.newStore(t)

			list := mustCreateList(t, store, testScope, openList("Launch"))
			signup := mustJoin(t, store, testScope, list.ID, &Signup{Contact: "ada@example.com"})

			must.NoError(t, store.Invite(t.Context(), testScope, list.ID, signup.ID))

			invited, err := store.GetSignup(t.Context(), testScope, list.ID, signup.ID)
			must.NoError(t, err)
			must.NotNil(t, invited.StatusChangedAt)

			must.NoError(t, store.UpdateSignupNotes(t.Context(), testScope, list.ID, signup.ID, "typo fixed"))

			read, err := store.GetSignup(t.Context(), testScope, list.ID, signup.ID)
			must.NoError(t, err)
			test.EqOp(t, "typo fixed", read.Notes)
			test.EqOp(t, StatusInvited, read.Status)

			// The reminder that goes out three days after an invitation is
			// scheduled off StatusChangedAt, and a typo must not reschedule it.
			must.NotNil(t, read.StatusChangedAt)
			test.EqOp(t, *invited.StatusChangedAt, *read.StatusChangedAt)
		})

		T.Run("does not cross lists or scopes", func(t *testing.T) {
			t.Parallel()

			store := env.newStore(t)

			list := mustCreateList(t, store, testScope, openList("Launch"))
			other := mustCreateList(t, store, testScope, openList("other"))
			signup := mustJoin(t, store, testScope, list.ID, &Signup{Contact: "ada@example.com"})

			test.ErrorIs(t, store.UpdateSignupNotes(t.Context(), otherScope, list.ID, signup.ID, "x"), ErrSignupNotFound)
			test.ErrorIs(t, store.UpdateSignupNotes(t.Context(), testScope, other.ID, signup.ID, "x"), ErrSignupNotFound)
			test.ErrorIs(t, store.UpdateSignupNotes(t.Context(), testScope, list.ID, "", "x"),
				platformerrors.ErrInvalidIDProvided)
		})
	})

	t.Run("Invite and Convert", func(T *testing.T) {
		T.Run("walk the lifecycle and stamp each move", func(t *testing.T) {
			t.Parallel()

			c := newStubClock()
			store := env.newStore(t, WithClock(c))

			list := mustCreateList(t, store, testScope, openList("Launch"))
			signup := mustJoin(t, store, testScope, list.ID, &Signup{Contact: "ada@example.com"})

			c.advance(time.Hour)
			must.NoError(t, store.Invite(t.Context(), testScope, list.ID, signup.ID))

			invited, err := store.GetSignup(t.Context(), testScope, list.ID, signup.ID)
			must.NoError(t, err)
			test.EqOp(t, StatusInvited, invited.Status)
			must.NotNil(t, invited.StatusChangedAt)

			c.advance(time.Hour)
			must.NoError(t, store.Convert(t.Context(), testScope, list.ID, signup.ID))

			converted, err := store.GetSignup(t.Context(), testScope, list.ID, signup.ID)
			must.NoError(t, err)
			test.EqOp(t, StatusConverted, converted.Status)
			must.NotNil(t, converted.StatusChangedAt)
			test.True(t, converted.StatusChangedAt.After(*invited.StatusChangedAt))
		})

		T.Run("happen once, and say what the signup is instead", func(t *testing.T) {
			t.Parallel()

			store := env.newStore(t)

			list := mustCreateList(t, store, testScope, openList("Launch"))
			signup := mustJoin(t, store, testScope, list.ID, &Signup{Contact: "ada@example.com"})

			// Converting somebody who was never invited is the out-of-order
			// move, and the guard is what refuses it.
			test.ErrorIs(t, store.Convert(t.Context(), testScope, list.ID, signup.ID), ErrWrongStatus)

			must.NoError(t, store.Invite(t.Context(), testScope, list.ID, signup.ID))

			// The second invitation is the one that would send a second email.
			// It loses on the affected-row count rather than on a read.
			test.ErrorIs(t, store.Invite(t.Context(), testScope, list.ID, signup.ID), ErrWrongStatus)
		})

		T.Run("report a missing signup rather than a wrong status", func(t *testing.T) {
			t.Parallel()

			store := env.newStore(t)

			list := mustCreateList(t, store, testScope, openList("Launch"))
			signup := mustJoin(t, store, testScope, list.ID, &Signup{Contact: "ada@example.com"})

			// A statement that matched nothing cannot tell the two apart, which
			// is why the losing path reads once more to find out.
			test.ErrorIs(t, store.Invite(t.Context(), testScope, list.ID, "nope"), ErrSignupNotFound)
			test.ErrorIs(t, store.Invite(t.Context(), otherScope, list.ID, signup.ID), ErrSignupNotFound)
			test.ErrorIs(t, store.Invite(t.Context(), testScope, list.ID, ""), platformerrors.ErrInvalidIDProvided)
		})
	})

	t.Run("ArchiveSignup", func(T *testing.T) {
		T.Run("hides the row and suppresses nothing", func(t *testing.T) {
			t.Parallel()

			store := env.newStore(t)

			list := mustCreateList(t, store, testScope, openList("Launch"))
			signup := mustJoin(t, store, testScope, list.ID, &Signup{Contact: "ada@example.com"})

			must.NoError(t, store.ArchiveSignup(t.Context(), testScope, list.ID, signup.ID))

			_, err := store.GetSignup(t.Context(), testScope, list.ID, signup.ID)
			test.ErrorIs(t, err, ErrSignupNotFound)

			page, err := store.ListSignups(t.Context(), testScope, list.ID, nil)
			must.NoError(t, err)
			test.SliceEmpty(t, page.Data)

			// Archiving is not withdrawing: nothing was erased, so what the
			// next attempt gets is the collision rather than the suppression.
			_, err = store.Join(t.Context(), testScope, list.ID, &Signup{Contact: "ada@example.com"})
			test.ErrorIs(t, err, ErrAlreadySignedUp)

			test.ErrorIs(t, store.ArchiveSignup(t.Context(), testScope, list.ID, signup.ID), ErrSignupNotFound)
			test.ErrorIs(t, store.ArchiveSignup(t.Context(), testScope, list.ID, ""),
				platformerrors.ErrInvalidIDProvided)
		})
	})
}
