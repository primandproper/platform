package waitlists

import (
	"testing"

	"github.com/primandproper/platform-go/v14/database"
	platformerrors "github.com/primandproper/platform-go/v14/errors"
	"github.com/primandproper/platform-go/v14/filtering"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// errCompanionWrite stands in for the consumer's own write failing beside one
// of this package's — the audit entry that would not go in, the outbox row that
// would not enqueue.
var errCompanionWrite = platformerrors.New("the companion write failed")

// runTransactionSuite is every write run inside a transaction the caller owns,
// which is what the Tx variants exist for.
//
// What is actually under test is the commit boundary: that a row written here
// lands with the caller's own rows, and that a caller's failure takes the row
// back with it. Everything else is parity — the transactional path must refuse
// exactly what its own non-transactional twin refuses, or the two drift into
// being two stores.
func runTransactionSuite(t *testing.T, env *storeEnv) {
	t.Helper()

	t.Run("every write commits with the caller's transaction", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		renamed := mustCreateList(t, store, testScope, openList("before"))
		doomed := mustCreateList(t, store, testScope, openList("on the way out"))
		converted := mustJoin(t, store, testScope, renamed.ID, &Signup{Contact: "converted@example.com"})
		withdrawn := mustJoin(t, store, testScope, renamed.ID, &Signup{Contact: "withdrawn@example.com"})
		archived := mustJoin(t, store, testScope, renamed.ID, &Signup{Contact: "archived@example.com"})

		var (
			opened *List
			joined *Signup
		)

		must.NoError(t, store.client.WithTransaction(t.Context(), func(tx database.Tx) (err error) {
			if opened, err = store.CreateListTx(t.Context(), tx, testScope, openList("opened inside")); err != nil {
				return err
			}

			renamed.Name = "after"
			if err = store.UpdateListTx(t.Context(), tx, testScope, renamed); err != nil {
				return err
			}

			if err = store.ArchiveListTx(t.Context(), tx, testScope, doomed.ID); err != nil {
				return err
			}

			if joined, err = store.JoinTx(t.Context(), tx, testScope, renamed.ID,
				&Signup{Contact: "joined@example.com", Subject: testSubject}); err != nil {
				return err
			}

			if err = store.UpdateSignupNotesTx(t.Context(), tx, testScope, renamed.ID, joined.ID, "noted"); err != nil {
				return err
			}

			if err = store.InviteTx(t.Context(), tx, testScope, renamed.ID, converted.ID); err != nil {
				return err
			}

			if err = store.ConvertTx(t.Context(), tx, testScope, renamed.ID, converted.ID); err != nil {
				return err
			}

			if err = store.WithdrawTx(t.Context(), tx, testScope, renamed.ID, withdrawn.ID); err != nil {
				return err
			}

			return store.ArchiveSignupTx(t.Context(), tx, testScope, renamed.ID, archived.ID)
		}))

		// The creates read their creation times back through the caller's
		// executor, so the values the caller is handed are the rows this
		// transaction wrote rather than zero times waiting on a commit.
		must.NotNil(t, opened)
		test.NotEqOp(t, "", opened.ID)
		test.False(t, opened.CreatedAt.IsZero())
		must.NotNil(t, joined)
		test.False(t, joined.CreatedAt.IsZero())

		list, err := store.GetList(t.Context(), testScope, opened.ID)
		must.NoError(t, err)
		test.EqOp(t, "opened inside", list.Name)

		list, err = store.GetList(t.Context(), testScope, renamed.ID)
		must.NoError(t, err)
		test.EqOp(t, "after", list.Name)

		_, err = store.GetList(t.Context(), testScope, doomed.ID)
		must.ErrorIs(t, err, ErrListNotFound)

		signup, err := store.GetSignup(t.Context(), testScope, renamed.ID, joined.ID)
		must.NoError(t, err)
		test.EqOp(t, "noted", signup.Notes)
		test.EqOp(t, StatusWaiting, signup.Status)

		signup, err = store.GetSignup(t.Context(), testScope, renamed.ID, converted.ID)
		must.NoError(t, err)
		test.EqOp(t, StatusConverted, signup.Status)

		signup, err = store.GetSignup(t.Context(), testScope, renamed.ID, withdrawn.ID)
		must.NoError(t, err)
		test.EqOp(t, StatusWithdrawn, signup.Status)
		test.EqOp(t, "", signup.Contact)

		_, err = store.GetSignup(t.Context(), testScope, renamed.ID, archived.ID)
		must.ErrorIs(t, err, ErrSignupNotFound)
	})

	t.Run("a rolled back transaction takes every write with it", func(t *testing.T) {
		t.Parallel()

		// This is the whole point of the variants, seen from the side that
		// matters: the consumer's companion write fails, and the row goes back
		// with it rather than surviving in a transaction it was never part of.
		store := env.newStore(t)

		kept := mustCreateList(t, store, testScope, openList("the original"))
		spared := mustCreateList(t, store, testScope, openList("still here"))
		waiting := mustJoin(t, store, testScope, kept.ID, &Signup{Contact: "waiting@example.com"})
		staying := mustJoin(t, store, testScope, kept.ID, &Signup{Contact: "staying@example.com", Notes: "as written"})
		visible := mustJoin(t, store, testScope, kept.ID, &Signup{Contact: "visible@example.com"})

		var (
			opened *List
			joined *Signup
		)

		err := store.client.WithTransaction(t.Context(), func(tx database.Tx) (err error) {
			if opened, err = store.CreateListTx(t.Context(), tx, testScope, openList("never committed")); err != nil {
				return err
			}

			edited := *kept
			edited.Name = "the edit"
			if err = store.UpdateListTx(t.Context(), tx, testScope, &edited); err != nil {
				return err
			}

			if err = store.ArchiveListTx(t.Context(), tx, testScope, spared.ID); err != nil {
				return err
			}

			if joined, err = store.JoinTx(t.Context(), tx, testScope, kept.ID,
				&Signup{Contact: "joined@example.com"}); err != nil {
				return err
			}

			if err = store.UpdateSignupNotesTx(t.Context(), tx, testScope, kept.ID, staying.ID, "the edit"); err != nil {
				return err
			}

			if err = store.InviteTx(t.Context(), tx, testScope, kept.ID, waiting.ID); err != nil {
				return err
			}

			if err = store.WithdrawTx(t.Context(), tx, testScope, kept.ID, staying.ID); err != nil {
				return err
			}

			if err = store.ArchiveSignupTx(t.Context(), tx, testScope, kept.ID, visible.ID); err != nil {
				return err
			}

			return errCompanionWrite
		})
		must.ErrorIs(t, err, errCompanionWrite)

		// The ids were minted onto the returned values on the way through.
		// Nothing undoes that, and nothing should: what rolled back is the row.
		must.NotNil(t, opened)
		must.NotNil(t, joined)

		_, err = store.GetList(t.Context(), testScope, opened.ID)
		must.ErrorIs(t, err, ErrListNotFound)

		list, err := store.GetList(t.Context(), testScope, kept.ID)
		must.NoError(t, err)
		test.EqOp(t, "the original", list.Name)

		list, err = store.GetList(t.Context(), testScope, spared.ID)
		must.NoError(t, err)
		test.Nil(t, list.ArchivedAt)

		_, err = store.GetSignup(t.Context(), testScope, kept.ID, joined.ID)
		must.ErrorIs(t, err, ErrSignupNotFound)

		signup, err := store.GetSignup(t.Context(), testScope, kept.ID, waiting.ID)
		must.NoError(t, err)
		test.EqOp(t, StatusWaiting, signup.Status)

		signup, err = store.GetSignup(t.Context(), testScope, kept.ID, staying.ID)
		must.NoError(t, err)
		test.EqOp(t, StatusWaiting, signup.Status)
		test.EqOp(t, "staying@example.com", signup.Contact)
		test.EqOp(t, "as written", signup.Notes)

		signup, err = store.GetSignup(t.Context(), testScope, kept.ID, visible.ID)
		must.NoError(t, err)
		test.Nil(t, signup.ArchivedAt)
	})

	t.Run("a signup joins a list opened in the same transaction", func(t *testing.T) {
		t.Parallel()

		// The list read and the suppression check run on the caller's executor
		// rather than on a transaction of the store's own, so a launch opened
		// and seeded in one transaction resolves instead of reporting the list
		// absent.
		store := env.newStore(t)

		var (
			opened *List
			joined *Signup
		)

		must.NoError(t, store.client.WithTransaction(t.Context(), func(tx database.Tx) (err error) {
			if opened, err = store.CreateListTx(t.Context(), tx, testScope, openList("Launch")); err != nil {
				return err
			}

			joined, err = store.JoinTx(t.Context(), tx, testScope, opened.ID, &Signup{Contact: "ada@example.com"})

			return err
		}))

		signup, err := store.GetSignup(t.Context(), testScope, opened.ID, joined.ID)
		must.NoError(t, err)
		test.EqOp(t, opened.ID, signup.ListID)
	})

	t.Run("a lost transition explains itself against the transaction's own row", func(t *testing.T) {
		t.Parallel()

		// The read on the losing path goes through the executor the write ran
		// on. Anything else would be reading a row this transaction has not
		// committed — and on a one-connection SQLite client, would be waiting
		// on itself for the connection to do it with.
		store := env.newStore(t)

		list := mustCreateList(t, store, testScope, openList("Launch"))

		var invitedTwice, convertedFirst, withdrawnTwice error

		must.NoError(t, store.client.WithTransaction(t.Context(), func(tx database.Tx) error {
			joined, err := store.JoinTx(t.Context(), tx, testScope, list.ID, &Signup{Contact: "ada@example.com"})
			if err != nil {
				return err
			}

			convertedFirst = store.ConvertTx(t.Context(), tx, testScope, list.ID, joined.ID)

			if err = store.InviteTx(t.Context(), tx, testScope, list.ID, joined.ID); err != nil {
				return err
			}

			invitedTwice = store.InviteTx(t.Context(), tx, testScope, list.ID, joined.ID)

			if err = store.WithdrawTx(t.Context(), tx, testScope, list.ID, joined.ID); err != nil {
				return err
			}

			withdrawnTwice = store.WithdrawTx(t.Context(), tx, testScope, list.ID, joined.ID)

			return nil
		}))

		must.ErrorIs(t, convertedFirst, ErrWrongStatus)
		must.ErrorIs(t, invitedTwice, ErrWrongStatus)
		must.ErrorIs(t, withdrawnTwice, ErrAlreadyWithdrawn)
	})

	t.Run("a transactional write refuses a nil executor", func(t *testing.T) {
		t.Parallel()

		// Every one of them, not a representative one: a variant that reached
		// for the store's writer when handed nothing would be a write outside
		// the transaction its caller believes it is in.
		store := env.newStore(t)

		_, err := store.CreateListTx(t.Context(), nil, testScope, openList("Launch"))
		must.ErrorIs(t, err, ErrNilExecutor)
		must.ErrorIs(t, store.UpdateListTx(t.Context(), nil, testScope, openList("Launch")), ErrNilExecutor)
		must.ErrorIs(t, store.ArchiveListTx(t.Context(), nil, testScope, "wl_1"), ErrNilExecutor)

		_, err = store.JoinTx(t.Context(), nil, testScope, "wl_1", &Signup{Contact: "ada@example.com"})
		must.ErrorIs(t, err, ErrNilExecutor)
		must.ErrorIs(t, store.UpdateSignupNotesTx(t.Context(), nil, testScope, "wl_1", "sg_1", "note"), ErrNilExecutor)
		must.ErrorIs(t, store.InviteTx(t.Context(), nil, testScope, "wl_1", "sg_1"), ErrNilExecutor)
		must.ErrorIs(t, store.ConvertTx(t.Context(), nil, testScope, "wl_1", "sg_1"), ErrNilExecutor)
		must.ErrorIs(t, store.WithdrawTx(t.Context(), nil, testScope, "wl_1", "sg_1"), ErrNilExecutor)
		must.ErrorIs(t, store.ArchiveSignupTx(t.Context(), nil, testScope, "wl_1", "sg_1"), ErrNilExecutor)

		_, err = store.WithdrawSignupsForSubject(t.Context(), nil, testScope, testSubject)
		must.ErrorIs(t, err, ErrNilExecutor)

		// And it wraps the module-wide sentinel, so a caller checking for a
		// nil input generally is answered too.
		must.ErrorIs(t, err, platformerrors.ErrNilInputParameter)
	})

	t.Run("the transactional writes refuse what their own path refuses", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		closed := mustCreateList(t, store, testScope, closedList("closed"))
		open := mustCreateList(t, store, testScope, openList("open"))
		left := mustJoin(t, store, testScope, open.ID, &Signup{Contact: "left@example.com"})
		must.NoError(t, store.Withdraw(t.Context(), testScope, open.ID, left.ID))

		// Collected inside one transaction and asserted outside it, so a failed
		// check does not abort the transaction the next one needs. None of
		// these is a statement the database refuses — each is a check this
		// package makes, or a statement that matched nothing.
		var (
			nilList, nameless, nilSignup, blankContact, halfSubject      error
			closedJoin, missingJoin, withdrawnJoin, takenJoin            error
			missingUpdate, missingArchiveList, missingNotes, missingMove error
			missingWithdraw, missingArchiveSignup, anonymousErasure      error
		)

		must.NoError(t, store.client.WithTransaction(t.Context(), func(tx database.Tx) error {
			_, nilList = store.CreateListTx(t.Context(), tx, testScope, nil)
			_, nameless = store.CreateListTx(t.Context(), tx, testScope, &List{ClosesAt: testNow})

			_, nilSignup = store.JoinTx(t.Context(), tx, testScope, open.ID, nil)
			_, blankContact = store.JoinTx(t.Context(), tx, testScope, open.ID, &Signup{Contact: "   "})
			_, halfSubject = store.JoinTx(t.Context(), tx, testScope, open.ID,
				&Signup{Contact: "half@example.com", Subject: Subject{Type: SubjectUser}})
			_, closedJoin = store.JoinTx(t.Context(), tx, testScope, closed.ID, &Signup{Contact: "late@example.com"})
			_, missingJoin = store.JoinTx(t.Context(), tx, testScope, "wl_never_written", &Signup{Contact: "lost@example.com"})
			_, withdrawnJoin = store.JoinTx(t.Context(), tx, testScope, open.ID, &Signup{Contact: "Left@Example.com"})

			if _, err := store.JoinTx(t.Context(), tx, testScope, open.ID, &Signup{Contact: "taken@example.com"}); err != nil {
				return err
			}

			_, takenJoin = store.JoinTx(t.Context(), tx, testScope, open.ID, &Signup{Contact: "taken@example.com"})

			absent := openList("an edit to nothing")
			absent.ID = "wl_never_written"
			missingUpdate = store.UpdateListTx(t.Context(), tx, testScope, absent)
			missingArchiveList = store.ArchiveListTx(t.Context(), tx, testScope, "wl_never_written")

			missingNotes = store.UpdateSignupNotesTx(t.Context(), tx, testScope, open.ID, "sg_never_written", "note")
			missingMove = store.InviteTx(t.Context(), tx, testScope, open.ID, "sg_never_written")
			missingWithdraw = store.WithdrawTx(t.Context(), tx, testScope, open.ID, "sg_never_written")
			missingArchiveSignup = store.ArchiveSignupTx(t.Context(), tx, testScope, open.ID, "sg_never_written")

			_, anonymousErasure = store.WithdrawSignupsForSubject(t.Context(), tx, testScope, Subject{})

			return nil
		}))

		must.ErrorIs(t, nilList, ErrNilList)
		must.ErrorIs(t, nameless, ErrEmptyListName)
		must.ErrorIs(t, nilSignup, ErrNilSignup)
		must.ErrorIs(t, blankContact, ErrEmptyContact)
		must.ErrorIs(t, halfSubject, ErrEmptySubjectID)
		must.ErrorIs(t, closedJoin, ErrListClosed)
		must.ErrorIs(t, missingJoin, ErrListNotFound)
		must.ErrorIs(t, withdrawnJoin, ErrContactWithdrawn)
		must.ErrorIs(t, takenJoin, ErrAlreadySignedUp)
		must.ErrorIs(t, missingUpdate, ErrListNotFound)
		must.ErrorIs(t, missingArchiveList, ErrListNotFound)
		must.ErrorIs(t, missingNotes, ErrSignupNotFound)
		must.ErrorIs(t, missingMove, ErrSignupNotFound)
		must.ErrorIs(t, missingWithdraw, ErrSignupNotFound)
		must.ErrorIs(t, missingArchiveSignup, ErrSignupNotFound)
		must.ErrorIs(t, anonymousErasure, ErrEmptySubjectType)

		// The one refusal made inside the transaction and committed by it: the
		// suppression check saw the row this transaction had just written.
		page, err := store.ListSignups(t.Context(), testScope, open.ID, nil)
		must.NoError(t, err)
		must.SliceLen(t, 2, page.Data)
	})

	t.Run("the erasure and the writes it follows share the caller's transaction", func(t *testing.T) {
		t.Parallel()

		// The shape a data privacy erasure has: several domains' erasers, one
		// transaction, and this one's rows withdrawn or restored with the rest.
		store := env.newStore(t)

		list := mustCreateList(t, store, testScope, openList("Launch"))
		mine := mustJoin(t, store, testScope, list.ID, &Signup{Contact: "ada@example.com", Subject: testSubject})

		err := store.client.WithTransaction(t.Context(), func(tx database.Tx) error {
			withdrawn, txErr := store.WithdrawSignupsForSubject(t.Context(), tx, testScope, testSubject)
			if txErr != nil {
				return txErr
			}

			test.EqOp(t, int64(1), withdrawn)

			return errCompanionWrite
		})
		must.ErrorIs(t, err, errCompanionWrite)

		signup, err := store.GetSignup(t.Context(), testScope, list.ID, mine.ID)
		must.NoError(t, err)
		test.EqOp(t, StatusWaiting, signup.Status)
		test.EqOp(t, "ada@example.com", signup.Contact)
		test.EqOp(t, testSubject, signup.Subject)

		everything := filtering.DefaultQueryFilter()
		everything.IncludeArchived = new(true)

		page, err := store.ListSignupsForSubject(t.Context(), testScope, testSubject, everything)
		must.NoError(t, err)
		must.SliceLen(t, 1, page.Data)
	})
}
