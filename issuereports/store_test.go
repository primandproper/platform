package issuereports

import (
	"testing"
	"time"

	"github.com/primandproper/platform-go/v14/database"
	"github.com/primandproper/platform-go/v14/database/dialect"
	platformerrors "github.com/primandproper/platform-go/v14/errors"
	"github.com/primandproper/platform-go/v14/filtering"
	"github.com/primandproper/platform-go/v14/pointer"
	"github.com/primandproper/platform-go/v14/tenancy"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// TestSQLStore_SQLite runs the behavioral suite against SQLite, which is the
// engine every developer has. TestSQLStore_RealServers runs the identical suite
// against Postgres and MySQL — see containers_test.go.
func TestSQLStore_SQLite(T *testing.T) {
	T.Parallel()

	runStoreSuite(T, newSQLiteEnv(T))
}

// runStoreSuite is everything this store promises, run against whichever
// database it is handed.
//
// It is one function rather than a file of top-level tests because the three
// engines have to be held to the same behavior: the placeholder rendering, the
// archived predicates and the :execrows count every guarded write reads as its
// answer are spelled three ways, and a suite that ran only against SQLite would
// prove the one spelling SQLite accepts.
func runStoreSuite(t *testing.T, env *storeEnv) {
	t.Helper()

	t.Run("writes", func(t *testing.T) {
		t.Parallel()

		runWriteSuite(t, env)
	})

	t.Run("lifecycle", func(t *testing.T) {
		t.Parallel()

		runLifecycleSuite(t, env)
	})

	t.Run("reads", func(t *testing.T) {
		t.Parallel()

		runReadSuite(t, env)
	})

	t.Run("erasure", func(t *testing.T) {
		t.Parallel()

		runErasureSuite(t, env)
	})
}

func runWriteSuite(t *testing.T, env *storeEnv) {
	t.Helper()

	t.Run("a created report comes back with what the database assigned", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		r := newReport(testReporter, "bug", "the button does nothing")
		must.NoError(t, store.CreateReport(t.Context(), r))

		// The id is minted here and the creation time is the database's, read
		// back rather than left as a zero time a caller would serialize as a
		// date in the year one.
		test.NotEqOp(t, "", r.ID)
		test.False(t, r.CreatedAt.IsZero())
		test.EqOp(t, StatusOpen, r.Status)
		test.Nil(t, r.ClosedAt)

		read, err := store.GetReport(t.Context(), testScope, r.ID)
		must.NoError(t, err)
		test.EqOp(t, r.ID, read.ID)
		test.EqOp(t, "bug", read.Kind)
		test.EqOp(t, "the button does nothing", read.Details)
		test.EqOp(t, "recipes", read.SubjectType)
		test.EqOp(t, "recipe_1", read.SubjectID)
		test.EqOp(t, StatusOpen, read.Status)
		test.EqOp(t, "", read.Resolution)
	})

	t.Run("a caller-supplied id is kept", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		r := newReport(testReporter, "bug", "details")
		r.ID = "report_of_my_own"
		must.NoError(t, store.CreateReport(t.Context(), r))

		read, err := store.GetReport(t.Context(), testScope, "report_of_my_own")
		must.NoError(t, err)
		test.EqOp(t, "report_of_my_own", read.ID)
	})

	t.Run("a report is born open and cannot be created in any other status", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		r := newReport(testReporter, "bug", "details")
		r.Status = StatusResolved

		err := store.CreateReport(t.Context(), r)
		must.ErrorIs(t, err, ErrInvalidStatusTransition)
	})

	t.Run("a resolution supplied at creation is dropped", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		closed := baseTime
		r := newReport(testReporter, "bug", "details")
		r.Resolution = "already fixed"
		r.ClosedAt = &closed

		must.NoError(t, store.CreateReport(t.Context(), r))
		test.EqOp(t, "", r.Resolution)
		test.Nil(t, r.ClosedAt)

		read, err := store.GetReport(t.Context(), testScope, r.ID)
		must.NoError(t, err)
		test.EqOp(t, "", read.Resolution)
		test.Nil(t, read.ClosedAt)
	})

	t.Run("a report with nothing in it is refused", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		for name, mutate := range map[string]func(*Report){
			"no reporter": func(r *Report) { r.Reporter = "" },
			"no kind":     func(r *Report) { r.Kind = "" },
			"no details":  func(r *Report) { r.Details = "" },
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				r := newReport(testReporter, "bug", "details")
				mutate(r)

				test.Error(t, store.CreateReport(t.Context(), r))
			})
		}
	})

	t.Run("a nil report is an error rather than a panic", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		must.ErrorIs(t, store.CreateReport(t.Context(), nil), ErrNilReport)
		must.ErrorIs(t, store.UpdateReport(t.Context(), nil), ErrNilReport)
	})

	t.Run("an update revises what the reporter said", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		r := filed(t, store, newReport(testReporter, "bug", "the button does nothing"))

		r.Kind = "typo"
		r.Details = "the label is misspelled"
		r.SubjectType = "labels"
		r.SubjectID = "label_9"
		must.NoError(t, store.UpdateReport(t.Context(), r))

		read, err := store.GetReport(t.Context(), testScope, r.ID)
		must.NoError(t, err)
		test.EqOp(t, "typo", read.Kind)
		test.EqOp(t, "the label is misspelled", read.Details)
		test.EqOp(t, "labels", read.SubjectType)
		test.EqOp(t, "label_9", read.SubjectID)
		must.NotNil(t, read.LastUpdatedAt)
	})

	t.Run("an update cannot move the status", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		r := filed(t, store, newReport(testReporter, "bug", "details"))

		// The value carries a status the caller changed, and the statement's SET
		// list does not name the column — so the row does not move.
		r.Status = StatusResolved
		must.NoError(t, store.UpdateReport(t.Context(), r))

		read, err := store.GetReport(t.Context(), testScope, r.ID)
		must.NoError(t, err)
		test.EqOp(t, StatusOpen, read.Status)
	})

	t.Run("an update in another scope finds nothing", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		r := filed(t, store, newReport(testReporter, "bug", "details"))

		trespass := *r
		trespass.Scope = otherScope
		trespass.Details = "rewritten by somebody else"

		must.ErrorIs(t, store.UpdateReport(t.Context(), &trespass), ErrReportNotFound)

		read, err := store.GetReport(t.Context(), testScope, r.ID)
		must.NoError(t, err)
		test.EqOp(t, "details", read.Details)
	})

	t.Run("archiving takes a report out of the queue, once", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		r := filed(t, store, newReport(testReporter, "bug", "details"))

		must.NoError(t, store.ArchiveReport(t.Context(), testScope, r.ID))

		_, err := store.GetReport(t.Context(), testScope, r.ID)
		must.ErrorIs(t, err, ErrReportNotFound)

		// A second archive addresses a report that is no longer in the queue.
		must.ErrorIs(t, store.ArchiveReport(t.Context(), testScope, r.ID), ErrReportNotFound)
	})

	t.Run("an archived report is still readable through the filter", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		r := filed(t, store, newReport(testReporter, "bug", "details"))
		must.NoError(t, store.ArchiveReport(t.Context(), testScope, r.ID))

		filter := filtering.DefaultQueryFilter()
		filter.IncludeArchived = pointer.To(true)

		page, err := store.ListReports(t.Context(), testScope, filter)
		must.NoError(t, err)
		must.SliceLen(t, 1, page.Data)
		must.NotNil(t, page.Data[0].ArchivedAt)
	})

	t.Run("a scopeless write is a refusal rather than a global one", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		r := newReport(testReporter, "bug", "details")
		// The zero Scope is the absence of a decision, not the global one.
		r.Scope = tenancy.Scope{}

		must.ErrorIs(t, store.CreateReport(t.Context(), r), tenancy.ErrNoScope)
	})
}

func runLifecycleSuite(t *testing.T, env *storeEnv) {
	t.Helper()

	t.Run("a report moves through the lifecycle and stamps when it stopped", func(t *testing.T) {
		t.Parallel()

		c := newStubClock()
		store := env.newStore(t, WithClock(c))
		r := filed(t, store, newReport(testReporter, "bug", "details"))

		acknowledged, err := store.TransitionReport(t.Context(), testScope, r.ID,
			StatusOpen, StatusAcknowledged, "looking at it")
		must.NoError(t, err)
		test.EqOp(t, StatusAcknowledged, acknowledged.Status)
		// Not terminal, so nothing is stamped and the note is not kept: a report
		// somebody has picked up has no resolution yet.
		test.Nil(t, acknowledged.ClosedAt)
		test.EqOp(t, "", acknowledged.Resolution)

		c.advance(90 * time.Minute)

		resolved, err := store.TransitionReport(t.Context(), testScope, r.ID,
			StatusAcknowledged, StatusResolved, "fixed in 1.4")
		must.NoError(t, err)
		test.EqOp(t, StatusResolved, resolved.Status)
		test.EqOp(t, "fixed in 1.4", resolved.Resolution)
		must.NotNil(t, resolved.ClosedAt)
		test.True(t, resolved.Closed())
		test.EqOp(t, baseTime.Add(90*time.Minute), resolved.ClosedAt.UTC())
	})

	t.Run("reopening clears the stamp and the reason it no longer holds", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t, WithClock(newStubClock()))
		r := filed(t, store, newReport(testReporter, "bug", "details"))

		_, err := store.TransitionReport(t.Context(), testScope, r.ID,
			StatusOpen, StatusDeclined, "working as intended")
		must.NoError(t, err)

		reopened, err := store.TransitionReport(t.Context(), testScope, r.ID,
			StatusDeclined, StatusOpen, "the reporter says otherwise")
		must.NoError(t, err)
		test.EqOp(t, StatusOpen, reopened.Status)
		test.Nil(t, reopened.ClosedAt)
		test.EqOp(t, "", reopened.Resolution)
	})

	t.Run("the second of two triagers loses the race", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		r := filed(t, store, newReport(testReporter, "bug", "details"))

		_, err := store.TransitionReport(t.Context(), testScope, r.ID,
			StatusOpen, StatusResolved, "fixed")
		must.NoError(t, err)

		// The second triager is holding the row as they read it: still open.
		_, err = store.TransitionReport(t.Context(), testScope, r.ID,
			StatusOpen, StatusDeclined, "duplicate")
		must.ErrorIs(t, err, ErrStatusConflict)

		read, err := store.GetReport(t.Context(), testScope, r.ID)
		must.NoError(t, err)
		test.EqOp(t, StatusResolved, read.Status)
		test.EqOp(t, "fixed", read.Resolution)
	})

	t.Run("a transition of a report that is not there says so", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		_, err := store.TransitionReport(t.Context(), testScope, "nonesuch",
			StatusOpen, StatusResolved, "fixed")
		must.ErrorIs(t, err, ErrReportNotFound)
	})

	t.Run("a transition in another scope cannot reach the row", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		r := filed(t, store, newReport(testReporter, "bug", "details"))

		_, err := store.TransitionReport(t.Context(), otherScope, r.ID,
			StatusOpen, StatusResolved, "fixed")
		must.ErrorIs(t, err, ErrReportNotFound)

		read, err := store.GetReport(t.Context(), testScope, r.ID)
		must.NoError(t, err)
		test.EqOp(t, StatusOpen, read.Status)
	})

	t.Run("a move the lifecycle does not admit is refused before the write", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		r := filed(t, store, newReport(testReporter, "bug", "details"))

		_, err := store.TransitionReport(t.Context(), testScope, r.ID,
			StatusResolved, StatusAcknowledged, "")
		must.ErrorIs(t, err, ErrInvalidStatusTransition)

		_, err = store.TransitionReport(t.Context(), testScope, r.ID,
			StatusOpen, StatusOpen, "")
		must.ErrorIs(t, err, ErrInvalidStatusTransition)

		_, err = store.TransitionReport(t.Context(), testScope, r.ID,
			StatusOpen, Status("closed"), "")
		must.ErrorIs(t, err, ErrUnknownStatus)

		read, err := store.GetReport(t.Context(), testScope, r.ID)
		must.NoError(t, err)
		test.EqOp(t, StatusOpen, read.Status)
	})

	t.Run("an archived report has left the lifecycle", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		r := filed(t, store, newReport(testReporter, "bug", "details"))
		must.NoError(t, store.ArchiveReport(t.Context(), testScope, r.ID))

		_, err := store.TransitionReport(t.Context(), testScope, r.ID,
			StatusOpen, StatusResolved, "fixed")
		must.ErrorIs(t, err, ErrReportNotFound)
	})
}

func runReadSuite(t *testing.T, env *storeEnv) {
	t.Helper()

	t.Run("a read cannot see another scope's reports", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		r := filed(t, store, newReport(testReporter, "bug", "details"))

		_, err := store.GetReport(t.Context(), otherScope, r.ID)
		must.ErrorIs(t, err, ErrReportNotFound)

		page, err := store.ListReports(t.Context(), otherScope, nil)
		must.NoError(t, err)
		test.SliceEmpty(t, page.Data)
	})

	t.Run("the triage queue is one status at a time and counts the whole of it", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		for i := range 3 {
			r := filed(t, store, newReport(testReporter, "bug", "details"))
			if i == 0 {
				_, err := store.TransitionReport(t.Context(), testScope, r.ID,
					StatusOpen, StatusResolved, "fixed")
				must.NoError(t, err)
			}
		}

		open, err := store.ListReportsByStatus(t.Context(), testScope, StatusOpen, nil)
		must.NoError(t, err)
		test.SliceLen(t, 2, open.Data)
		test.EqOp(t, uint64(2), open.FilteredCount)

		resolved, err := store.ListReportsByStatus(t.Context(), testScope, StatusResolved, nil)
		must.NoError(t, err)
		test.SliceLen(t, 1, resolved.Data)
	})

	t.Run("a queue nobody spelled right is refused rather than answered empty", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		_, err := store.ListReportsByStatus(t.Context(), testScope, Status("triaged"), nil)
		must.ErrorIs(t, err, ErrUnknownStatus)
	})

	t.Run("a reporter's own reports are theirs alone", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		mine := filed(t, store, newReport(testReporter, "bug", "mine"))
		filed(t, store, newReport(otherReporter, "bug", "theirs"))

		page, err := store.ListReportsByReporter(t.Context(), testScope, testReporter, nil)
		must.NoError(t, err)
		must.SliceLen(t, 1, page.Data)
		test.EqOp(t, mine.ID, page.Data[0].ID)

		_, err = store.ListReportsByReporter(t.Context(), testScope, "", nil)
		must.ErrorIs(t, err, ErrEmptyReporter)
	})

	t.Run("subject reads narrow from a kind of thing to one of them", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		first := filed(t, store, newReport(testReporter, "bug", "about recipe 1"))

		second := newReport(testReporter, "bug", "about recipe 2")
		second.SubjectID = "recipe_2"
		filed(t, store, second)

		elsewhere := newReport(testReporter, "bug", "about a label")
		elsewhere.SubjectType = "labels"
		elsewhere.SubjectID = "label_1"
		filed(t, store, elsewhere)

		byType, err := store.ListReportsBySubjectType(t.Context(), testScope, "recipes", nil)
		must.NoError(t, err)
		test.SliceLen(t, 2, byType.Data)

		bySubject, err := store.ListReportsForSubject(t.Context(), testScope, "recipes", "recipe_1", nil)
		must.NoError(t, err)
		must.SliceLen(t, 1, bySubject.Data)
		test.EqOp(t, first.ID, bySubject.Data[0].ID)
	})

	t.Run("a descending page is the ascending one reversed", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		ids := make([]string, 0, 3)
		for i := range 3 {
			r := newReport(testReporter, "bug", "details")
			r.ID = string(rune('a'+i)) + "_report"
			filed(t, store, r)
			ids = append(ids, r.ID)
		}

		ascending, err := store.ListReports(t.Context(), testScope, nil)
		must.NoError(t, err)
		must.SliceLen(t, 3, ascending.Data)
		test.EqOp(t, ids[0], ascending.Data[0].ID)

		filter := filtering.DefaultQueryFilter()
		filter.SortBy = filtering.SortDescending

		descending, err := store.ListReports(t.Context(), testScope, filter)
		must.NoError(t, err)
		must.SliceLen(t, 3, descending.Data)
		test.EqOp(t, ids[2], descending.Data[0].ID)
	})

	t.Run("a page walks its cursor to the end", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		for i := range 3 {
			r := newReport(testReporter, "bug", "details")
			r.ID = string(rune('a'+i)) + "_report"
			filed(t, store, r)
		}

		filter := filtering.DefaultQueryFilter()
		filter.MaxResponseSize = pointer.To(uint16(2))

		first, err := store.ListReports(t.Context(), testScope, filter)
		must.NoError(t, err)
		must.SliceLen(t, 2, first.Data)
		test.EqOp(t, uint64(3), first.FilteredCount)

		filter.SetCursor(&first.Cursor)

		second, err := store.ListReports(t.Context(), testScope, filter)
		must.NoError(t, err)
		test.SliceLen(t, 1, second.Data)
	})

	t.Run("a scopeless read is a refusal rather than a wider one", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		// The zero Scope is the absence of a decision, not the global one.
		blank := tenancy.Scope{}

		_, err := store.GetReport(t.Context(), blank, "whatever")
		test.Error(t, err)

		_, err = store.ListReports(t.Context(), blank, nil)
		test.Error(t, err)

		_, err = store.ListReportsByStatus(t.Context(), blank, StatusOpen, nil)
		test.Error(t, err)

		_, err = store.ListReportsByReporter(t.Context(), blank, testReporter, nil)
		test.Error(t, err)

		_, err = store.ListReportsBySubjectType(t.Context(), blank, "recipes", nil)
		test.Error(t, err)

		_, err = store.ListReportsForSubject(t.Context(), blank, "recipes", "recipe_1", nil)
		test.Error(t, err)
	})
}

func runErasureSuite(t *testing.T, env *storeEnv) {
	t.Helper()

	t.Run("an erasure destroys one reporter's reports and nobody else's", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		filed(t, store, newReport(testReporter, "bug", "mine"))

		// Archived and still theirs: an erasure has to reach what a soft delete
		// hid, or a subject's data survives their own request.
		archived := filed(t, store, newReport(testReporter, "bug", "archived but still mine"))
		must.NoError(t, store.ArchiveReport(t.Context(), testScope, archived.ID))

		theirs := filed(t, store, newReport(otherReporter, "bug", "theirs"))

		var deleted int64

		must.NoError(t, store.client.WithTransaction(t.Context(), func(tx database.Tx) error {
			var err error
			deleted, err = store.DeleteReportsByReporter(t.Context(), tx, testScope, testReporter)

			return err
		}))

		test.EqOp(t, int64(2), deleted)

		mine, err := store.ListReportsByReporter(t.Context(), testScope, testReporter, nil)
		must.NoError(t, err)
		test.SliceEmpty(t, mine.Data)

		survivor, err := store.GetReport(t.Context(), testScope, theirs.ID)
		must.NoError(t, err)
		test.EqOp(t, otherReporter, survivor.Reporter)
	})

	t.Run("an erasure cannot reach another scope", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		r := filed(t, store, newReport(testReporter, "bug", "details"))

		var deleted int64

		must.NoError(t, store.client.WithTransaction(t.Context(), func(tx database.Tx) error {
			var err error
			deleted, err = store.DeleteReportsByReporter(t.Context(), tx, otherScope, testReporter)

			return err
		}))

		test.EqOp(t, int64(0), deleted)

		_, err := store.GetReport(t.Context(), testScope, r.ID)
		must.NoError(t, err)
	})

	t.Run("a subject who filed nothing is not a failure", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		must.NoError(t, store.client.WithTransaction(t.Context(), func(tx database.Tx) error {
			deleted, err := store.DeleteReportsByReporter(t.Context(), tx, testScope, "never_filed_anything")
			test.EqOp(t, int64(0), deleted)

			return err
		}))
	})

	t.Run("an erasure without a transaction is refused", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		_, err := store.DeleteReportsByReporter(t.Context(), nil, testScope, testReporter)
		must.ErrorIs(t, err, ErrNilExecutor)
	})

	t.Run("an erasure naming nobody is refused", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		must.NoError(t, store.client.WithTransaction(t.Context(), func(tx database.Tx) error {
			_, err := store.DeleteReportsByReporter(t.Context(), tx, testScope, "")
			must.ErrorIs(t, err, ErrEmptyReporter)

			return nil
		}))
	})
}

func TestNewSQLStore(T *testing.T) {
	T.Parallel()

	T.Run("refuses a nil client", func(t *testing.T) {
		t.Parallel()

		_, err := NewSQLStore(nil)
		test.ErrorIs(t, err, ErrNilDatabaseClient)
		test.ErrorIs(t, err, platformerrors.ErrNilInputParameter)
	})

	T.Run("refuses a prefix that would not render an identifier", func(t *testing.T) {
		t.Parallel()

		env := newSQLiteEnv(t)

		_, err := NewSQLStore(env.client, WithTablePrefix("no-hyphens-allowed"))
		test.Error(t, err)
	})

	T.Run("reports the prefix it was built with", func(t *testing.T) {
		t.Parallel()

		env := newSQLiteEnv(t)

		store, err := NewSQLStore(env.client, WithTablePrefix("ir"))
		must.NoError(t, err)
		test.EqOp(t, "ir", store.TablePrefix())

		unprefixed, err := NewSQLStore(env.client)
		must.NoError(t, err)
		test.EqOp(t, DefaultTablePrefix, unprefixed.TablePrefix())
	})

	T.Run("ignores a nil option and a nil clock", func(t *testing.T) {
		t.Parallel()

		env := newSQLiteEnv(t)

		store, err := NewSQLStore(env.client, nil, WithClock(nil))
		must.NoError(t, err)
		must.NotNil(t, store)
		test.False(t, store.now().IsZero())
	})
}

func TestIssuereportsdbDialect(T *testing.T) {
	T.Parallel()

	T.Run("maps every dialect this module supports", func(t *testing.T) {
		t.Parallel()

		for _, d := range []dialect.Dialect{dialect.Postgres, dialect.MySQL, dialect.SQLite} {
			mapped, err := issuereportsdbDialect(d)
			must.NoError(t, err, must.Sprintf("dialect %s", d))
			test.NotEqOp(t, "", string(mapped))
		}
	})

	T.Run("names a dialect the querier was not generated for", func(t *testing.T) {
		t.Parallel()

		_, err := issuereportsdbDialect(dialect.Dialect("cassandra"))
		test.ErrorIs(t, err, dialect.ErrUnsupported)
	})
}
