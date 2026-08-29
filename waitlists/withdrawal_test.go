package waitlists

import (
	"testing"
	"time"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// runWithdrawalSuite is every assertion about the obligation this package is
// shaped around: somebody asks to come off a list, and stays off it.
//
// It is its own suite rather than a subtest of the signup one because it is the
// property the schema, the digest column and the guarded write all exist for,
// and a reader looking for "does the unsubscribe actually hold" should find it
// in one place.
func runWithdrawalSuite(t *testing.T, env *storeEnv) {
	t.Helper()

	t.Run("Withdraw", func(T *testing.T) {
		T.Run("erases what the row said about a person and keeps the digest", func(t *testing.T) {
			t.Parallel()

			c := newStubClock()
			store := env.newStore(t, WithClock(c))

			list := mustCreateList(t, store, testScope, openList("Launch"))
			signup := mustJoin(t, store, testScope, list.ID, &Signup{
				Contact: "ada@example.com",
				Notes:   "met at the conference",
				Subject: testSubject,
			})

			c.advance(time.Hour)
			must.NoError(t, store.Withdraw(t.Context(), testScope, list.ID, signup.ID))

			read, err := store.GetSignup(t.Context(), testScope, list.ID, signup.ID)
			must.NoError(t, err)

			test.EqOp(t, StatusWithdrawn, read.Status)
			test.True(t, read.Withdrawn())
			test.EqOp(t, "", read.Contact)
			test.EqOp(t, "", read.Notes)
			test.True(t, read.Subject.Anonymous())
			must.NotNil(t, read.StatusChangedAt)

			// The digest is the whole point: it is what remembers somebody the
			// table no longer holds.
			test.EqOp(t, signup.ContactDigest, read.ContactDigest)
		})

		T.Run("holds against a later signup from the same address", func(t *testing.T) {
			t.Parallel()

			store := env.newStore(t)

			list := mustCreateList(t, store, testScope, openList("Launch"))
			signup := mustJoin(t, store, testScope, list.ID, &Signup{Contact: "Ada@Example.com"})

			must.NoError(t, store.Withdraw(t.Context(), testScope, list.ID, signup.ID))

			// The failure a hand-rolled table has: delete the row and the next
			// form submission re-subscribes whoever asked to be left alone.
			_, err := store.Join(t.Context(), testScope, list.ID, &Signup{Contact: "ada@example.com"})
			test.ErrorIs(t, err, ErrContactWithdrawn)

			// And under a different capitalization, which is why the digest is
			// taken of Normalize's output rather than of the address as typed.
			_, err = store.Join(t.Context(), testScope, list.ID, &Signup{Contact: "  ADA@EXAMPLE.COM "})
			test.ErrorIs(t, err, ErrContactWithdrawn)
		})

		T.Run("is per list, not per person", func(t *testing.T) {
			t.Parallel()

			store := env.newStore(t)

			first := mustCreateList(t, store, testScope, openList("first"))
			second := mustCreateList(t, store, testScope, openList("second"))

			signup := mustJoin(t, store, testScope, first.ID, &Signup{Contact: "ada@example.com"})
			must.NoError(t, store.Withdraw(t.Context(), testScope, first.ID, signup.ID))

			// Coming off one list is not coming off every list. The uniqueness
			// and the suppression are both keyed on (scope, list, digest).
			mustJoin(t, store, testScope, second.ID, &Signup{Contact: "ada@example.com"})
		})

		T.Run("takes the row out of the subject's own reads", func(t *testing.T) {
			t.Parallel()

			store := env.newStore(t)

			list := mustCreateList(t, store, testScope, openList("Launch"))
			signup := mustJoin(t, store, testScope, list.ID, &Signup{
				Contact: "ada@example.com",
				Subject: testSubject,
			})

			must.NoError(t, store.Withdraw(t.Context(), testScope, list.ID, signup.ID))

			// The subject reference goes with the contact, so the row that
			// remembers a suppression no longer says whose it was.
			page, err := store.ListSignupsForSubject(t.Context(), testScope, testSubject, nil)
			must.NoError(t, err)
			test.SliceEmpty(t, page.Data)
		})

		T.Run("moves somebody at any point in the lifecycle", func(t *testing.T) {
			t.Parallel()

			store := env.newStore(t)

			list := mustCreateList(t, store, testScope, openList("Launch"))

			invited := mustJoin(t, store, testScope, list.ID, &Signup{Contact: "invited@example.com"})
			must.NoError(t, store.Invite(t.Context(), testScope, list.ID, invited.ID))
			must.NoError(t, store.Withdraw(t.Context(), testScope, list.ID, invited.ID))

			converted := mustJoin(t, store, testScope, list.ID, &Signup{Contact: "converted@example.com"})
			must.NoError(t, store.Invite(t.Context(), testScope, list.ID, converted.ID))
			must.NoError(t, store.Convert(t.Context(), testScope, list.ID, converted.ID))

			// Somebody who took the invitation up may still ask to stop hearing
			// from the list, so the withdrawal is guarded on not-yet-withdrawn
			// rather than on any particular status.
			must.NoError(t, store.Withdraw(t.Context(), testScope, list.ID, converted.ID))
		})

		T.Run("a replay reports rather than restamping", func(t *testing.T) {
			t.Parallel()

			c := newStubClock()
			store := env.newStore(t, WithClock(c))

			list := mustCreateList(t, store, testScope, openList("Launch"))
			signup := mustJoin(t, store, testScope, list.ID, &Signup{Contact: "ada@example.com"})

			must.NoError(t, store.Withdraw(t.Context(), testScope, list.ID, signup.ID))

			first, err := store.GetSignup(t.Context(), testScope, list.ID, signup.ID)
			must.NoError(t, err)
			must.NotNil(t, first.StatusChangedAt)

			c.advance(24 * time.Hour)
			test.ErrorIs(t, store.Withdraw(t.Context(), testScope, list.ID, signup.ID), ErrAlreadyWithdrawn)

			// The moment somebody asked to come off a list is a fact about
			// them, and a second request must not move it.
			second, err := store.GetSignup(t.Context(), testScope, list.ID, signup.ID)
			must.NoError(t, err)
			must.NotNil(t, second.StatusChangedAt)
			test.EqOp(t, *first.StatusChangedAt, *second.StatusChangedAt)
		})

		T.Run("does not cross lists or scopes", func(t *testing.T) {
			t.Parallel()

			store := env.newStore(t)

			list := mustCreateList(t, store, testScope, openList("Launch"))
			other := mustCreateList(t, store, testScope, openList("other"))
			signup := mustJoin(t, store, testScope, list.ID, &Signup{Contact: "ada@example.com"})

			test.ErrorIs(t, store.Withdraw(t.Context(), otherScope, list.ID, signup.ID), ErrSignupNotFound)
			test.ErrorIs(t, store.Withdraw(t.Context(), testScope, other.ID, signup.ID), ErrSignupNotFound)
		})

		T.Run("outlives the list closing", func(t *testing.T) {
			t.Parallel()

			c := newStubClock()
			store := env.newStore(t, WithClock(c))

			list := mustCreateList(t, store, testScope, openList("Launch"))
			signup := mustJoin(t, store, testScope, list.ID, &Signup{Contact: "ada@example.com"})

			c.advance(31 * 24 * time.Hour)

			// A closed list still lets somebody leave it. Nothing about the
			// withdrawal reads the list, which is deliberate: an obligation
			// that expired with the list would be an obligation.
			must.NoError(t, store.Withdraw(t.Context(), testScope, list.ID, signup.ID))
		})
	})
}
