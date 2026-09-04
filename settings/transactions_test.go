package settings

import (
	"testing"

	"github.com/primandproper/platform-go/v14/database"
	platformerrors "github.com/primandproper/platform-go/v14/errors"
	"github.com/primandproper/platform-go/v14/filtering"
	"github.com/primandproper/platform-go/v14/pointer"
	"github.com/primandproper/platform-go/v14/tenancy"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// runTransactionSuite is the five writes run inside a transaction the caller
// owns, which is what the Tx variants exist for.
//
// What is actually under test is the commit boundary: that a write made here
// lands with the caller's own rows, and that a caller's failure takes it back.
// Everything else is parity — the transactional path must refuse exactly what
// its own twin refuses, or the two drift into being two stores — except the one
// place the two paths are documented to differ, which is the stranded-value walk
// reading the caller's uncommitted clearances.
func runTransactionSuite(t *testing.T, env *storeEnv) {
	t.Helper()

	t.Run("the five writes commit with the caller's transaction", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		renamed := mustCreate(t, store, testScope, boolDefinition("compact"))
		retired := mustCreate(t, store, testScope, intDefinition("retention.days"))
		answered := mustCreate(t, store, testScope, stringDefinition("digest"))

		_, err := store.SetValue(t.Context(), testScope, testSubject, answered.Name, "daily")
		must.NoError(t, err)

		var (
			created *Definition
			value   *Value
		)

		must.NoError(t, store.client.WithTransaction(t.Context(), func(tx database.Tx) error {
			var txErr error

			// A definition and a value against it, in one transaction: the
			// definition read behind the value's write runs on tx, so it finds
			// a row nothing else can see yet.
			if created, txErr = store.CreateDefinitionTx(t.Context(), tx, testScope, stringDefinition("theme")); txErr != nil {
				return txErr
			}

			if value, txErr = store.SetValueTx(t.Context(), tx, testScope, testSubject, "theme", "never"); txErr != nil {
				return txErr
			}

			edit := *renamed
			edit.Name = "layout.compact"
			if txErr = store.UpdateDefinitionTx(t.Context(), tx, testScope, &edit); txErr != nil {
				return txErr
			}

			if txErr = store.ArchiveDefinitionTx(t.Context(), tx, testScope, retired.ID); txErr != nil {
				return txErr
			}

			return store.ClearValueTx(t.Context(), tx, testScope, testSubject, answered.Name)
		}))

		// The create read its creation time back through the caller's
		// executor, and the value's read-back is the row this transaction
		// wrote rather than a zero time waiting on a commit.
		test.NotEqOp(t, "", created.ID)
		test.False(t, created.CreatedAt.IsZero())
		test.EqOp(t, created.ID, value.DefinitionID)
		test.False(t, value.CreatedAt.IsZero())

		read, err := store.GetValue(t.Context(), testScope, testSubject, "theme")
		must.NoError(t, err)
		test.EqOp(t, "never", read.Raw)

		definition, err := store.GetDefinition(t.Context(), testScope, renamed.ID)
		must.NoError(t, err)
		test.EqOp(t, "layout.compact", definition.Name)

		_, err = store.GetDefinition(t.Context(), testScope, retired.ID)
		test.ErrorIs(t, err, ErrDefinitionNotFound)

		_, err = store.GetValue(t.Context(), testScope, testSubject, answered.Name)
		test.ErrorIs(t, err, ErrValueNotFound)
	})

	t.Run("a rolled back transaction takes all five writes with it", func(t *testing.T) {
		t.Parallel()

		// This is the whole point of the variants, seen from the side that
		// matters: the consumer's companion write fails, and every setting
		// write goes back with it rather than surviving in a transaction it was
		// never part of.
		store := env.newStore(t)

		renamed := mustCreate(t, store, testScope, boolDefinition("compact"))
		retired := mustCreate(t, store, testScope, intDefinition("retention.days"))
		answered := mustCreate(t, store, testScope, stringDefinition("digest"))

		_, err := store.SetValue(t.Context(), testScope, testSubject, answered.Name, "daily")
		must.NoError(t, err)

		var created *Definition

		err = store.client.WithTransaction(t.Context(), func(tx database.Tx) error {
			var txErr error

			if created, txErr = store.CreateDefinitionTx(t.Context(), tx, testScope, stringDefinition("theme")); txErr != nil {
				return txErr
			}

			if _, txErr = store.SetValueTx(t.Context(), tx, testScope, testSubject, "theme", "never"); txErr != nil {
				return txErr
			}

			edit := *renamed
			edit.Name = "layout.compact"
			if txErr = store.UpdateDefinitionTx(t.Context(), tx, testScope, &edit); txErr != nil {
				return txErr
			}

			if txErr = store.ArchiveDefinitionTx(t.Context(), tx, testScope, retired.ID); txErr != nil {
				return txErr
			}

			if txErr = store.ClearValueTx(t.Context(), tx, testScope, testSubject, answered.Name); txErr != nil {
				return txErr
			}

			return errCompanionWrite
		})
		must.ErrorIs(t, err, errCompanionWrite)

		// The id was minted onto the returned value on the way through. Nothing
		// undoes that, and nothing should: what rolled back is the row.
		test.NotEqOp(t, "", created.ID)

		_, err = store.GetDefinition(t.Context(), testScope, created.ID)
		test.ErrorIs(t, err, ErrDefinitionNotFound)

		_, err = store.GetDefinitionByName(t.Context(), testScope, "theme")
		test.ErrorIs(t, err, ErrDefinitionNotFound)

		definition, err := store.GetDefinition(t.Context(), testScope, renamed.ID)
		must.NoError(t, err)
		test.EqOp(t, "compact", definition.Name)
		test.Nil(t, definition.LastUpdatedAt)

		definition, err = store.GetDefinition(t.Context(), testScope, retired.ID)
		must.NoError(t, err)
		test.Nil(t, definition.ArchivedAt)

		read, err := store.GetValue(t.Context(), testScope, testSubject, answered.Name)
		must.NoError(t, err)
		test.EqOp(t, "daily", read.Raw)
		test.Nil(t, read.ArchivedAt)
	})

	t.Run("an edit sees the values the transaction has already cleared", func(t *testing.T) {
		t.Parallel()

		// The documented difference between the two paths. The walk runs on the
		// caller's executor, so an administrator can clear the offending
		// values and narrow the enumeration in one transaction — and the two
		// land together or not at all.
		store := env.newStore(t)
		definition := mustCreate(t, store, testScope, stringDefinition("digest"))

		_, err := store.SetValue(t.Context(), testScope, testSubject, "digest", "daily")
		must.NoError(t, err)

		narrowed := *definition
		narrowed.Enumeration = []string{"weekly", "never"}

		// Without the clearance, the transactional path refuses what the
		// other path refuses, and the refusal names the value.
		err = store.client.WithTransaction(t.Context(), func(tx database.Tx) error {
			return store.UpdateDefinitionTx(t.Context(), tx, testScope, &narrowed)
		})
		must.ErrorIs(t, err, ErrStrandedValues)
		test.StrContains(t, err.Error(), "daily")

		// With it, the walk sees the cleared row as cleared, and the edit goes
		// through.
		must.NoError(t, store.client.WithTransaction(t.Context(), func(tx database.Tx) error {
			if txErr := store.ClearValueTx(t.Context(), tx, testScope, testSubject, "digest"); txErr != nil {
				return txErr
			}

			return store.UpdateDefinitionTx(t.Context(), tx, testScope, &narrowed)
		}))

		read, err := store.GetDefinition(t.Context(), testScope, definition.ID)
		must.NoError(t, err)
		test.Eq(t, []string{"never", "weekly"}, read.Enumeration)

		_, err = store.GetValue(t.Context(), testScope, testSubject, "digest")
		test.ErrorIs(t, err, ErrValueNotFound)
	})

	t.Run("a transactional write refuses a nil executor", func(t *testing.T) {
		t.Parallel()

		// Every one of the six, not a representative one: a variant that
		// reached for the store's own writer when handed nothing would be a
		// write outside the transaction its caller believes it is in.
		store := env.newStore(t)

		_, err := store.CreateDefinitionTx(t.Context(), nil, testScope, stringDefinition("digest"))
		test.ErrorIs(t, err, ErrNilExecutor)

		test.ErrorIs(t, store.UpdateDefinitionTx(t.Context(), nil, testScope, stringDefinition("digest")), ErrNilExecutor)
		test.ErrorIs(t, store.ArchiveDefinitionTx(t.Context(), nil, testScope, "whatever"), ErrNilExecutor)

		_, err = store.SetValueTx(t.Context(), nil, testScope, testSubject, "digest", "daily")
		test.ErrorIs(t, err, ErrNilExecutor)

		test.ErrorIs(t, store.ClearValueTx(t.Context(), nil, testScope, testSubject, "digest"), ErrNilExecutor)

		_, err = store.DeleteValuesForSubject(t.Context(), nil, testScope, testSubject)
		test.ErrorIs(t, err, ErrNilExecutor)

		// And nothing was written on the way to refusing.
		_, err = store.GetDefinitionByName(t.Context(), testScope, "digest")
		test.ErrorIs(t, err, ErrDefinitionNotFound)
	})

	t.Run("the transactional writes refuse what their own path refuses", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		taken := mustCreate(t, store, testScope, stringDefinition("digest"))
		other := mustCreate(t, store, testScope, boolDefinition("compact"))

		var unset tenancy.Scope

		// Collected inside one transaction and asserted outside it, so a failed
		// check does not abort the transaction the next one needs. None of
		// these reaches a statement the database refuses: each is a check this
		// package makes, or a read that finds nothing.
		var (
			nilCreate, unscopedCreate, takenCreate, malformedCreate  error
			nilUpdate, unidentifiedUpdate, absentUpdate, takenUpdate error
			absentArchive, foreignArchive                            error
			unnamedSet, undefinedSet, unenumeratedSet                error
			unnamedClear, undefinedClear, unansweredClear            error
		)

		must.NoError(t, store.client.WithTransaction(t.Context(), func(tx database.Tx) error {
			_, nilCreate = store.CreateDefinitionTx(t.Context(), tx, testScope, nil)
			_, unscopedCreate = store.CreateDefinitionTx(t.Context(), tx, unset, stringDefinition("theme"))
			_, takenCreate = store.CreateDefinitionTx(t.Context(), tx, testScope, stringDefinition("digest"))

			malformed := stringDefinition("theme")
			malformed.Default = pointer.To("hourly")
			_, malformedCreate = store.CreateDefinitionTx(t.Context(), tx, testScope, malformed)

			nilUpdate = store.UpdateDefinitionTx(t.Context(), tx, testScope, nil)
			unidentifiedUpdate = store.UpdateDefinitionTx(t.Context(), tx, testScope, stringDefinition("digest"))

			absent := stringDefinition("digest")
			absent.ID = "def_never_written"
			absentUpdate = store.UpdateDefinitionTx(t.Context(), tx, testScope, absent)

			collision := *other
			collision.Name = taken.Name
			takenUpdate = store.UpdateDefinitionTx(t.Context(), tx, testScope, &collision)

			absentArchive = store.ArchiveDefinitionTx(t.Context(), tx, testScope, "def_never_written")
			foreignArchive = store.ArchiveDefinitionTx(t.Context(), tx, otherScope, taken.ID)

			_, unnamedSet = store.SetValueTx(t.Context(), tx, testScope, Subject{Type: SubjectUser}, "digest", "daily")
			_, undefinedSet = store.SetValueTx(t.Context(), tx, testScope, testSubject, "never.defined", "daily")
			_, unenumeratedSet = store.SetValueTx(t.Context(), tx, testScope, testSubject, "digest", "hourly")

			unnamedClear = store.ClearValueTx(t.Context(), tx, testScope, Subject{ID: "user-1"}, "digest")
			undefinedClear = store.ClearValueTx(t.Context(), tx, testScope, testSubject, "never.defined")
			unansweredClear = store.ClearValueTx(t.Context(), tx, testScope, testSubject, "digest")

			return nil
		}))

		test.ErrorIs(t, nilCreate, ErrNilDefinition)
		test.ErrorIs(t, unscopedCreate, tenancy.ErrNoScope)
		test.ErrorIs(t, takenCreate, ErrDefinitionNameTaken)
		test.ErrorIs(t, malformedCreate, ErrNotEnumerated)

		test.ErrorIs(t, nilUpdate, ErrNilDefinition)
		test.ErrorIs(t, unidentifiedUpdate, platformerrors.ErrInvalidIDProvided)
		test.ErrorIs(t, absentUpdate, ErrDefinitionNotFound)
		test.ErrorIs(t, takenUpdate, ErrDefinitionNameTaken)

		test.ErrorIs(t, absentArchive, ErrDefinitionNotFound)
		test.ErrorIs(t, foreignArchive, ErrDefinitionNotFound)

		test.ErrorIs(t, unnamedSet, ErrEmptySubjectID)
		test.ErrorIs(t, undefinedSet, ErrDefinitionNotFound)
		test.ErrorIs(t, unenumeratedSet, ErrNotEnumerated)

		test.ErrorIs(t, unnamedClear, ErrEmptySubjectType)
		test.ErrorIs(t, undefinedClear, ErrDefinitionNotFound)
		test.ErrorIs(t, unansweredClear, ErrValueNotFound)

		// The transaction committed with nothing in it: every refusal was
		// refused before a row changed.
		_, err := store.GetValue(t.Context(), testScope, testSubject, "digest")
		test.ErrorIs(t, err, ErrValueNotFound)

		_, err = store.GetDefinitionByName(t.Context(), testScope, "theme")
		test.ErrorIs(t, err, ErrDefinitionNotFound)
	})
}

// runErasureSuite is DeleteValuesForSubject: the one delete here, and what an
// erasure is built on.
func runErasureSuite(t *testing.T, env *storeEnv) {
	t.Helper()

	t.Run("an erasure removes a subject's answers, cleared ones included", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		mustCreate(t, store, testScope, stringDefinition("digest"))
		mustCreate(t, store, testScope, boolDefinition("compact"))
		mustCreate(t, store, otherScope, stringDefinition("digest"))

		second := Subject{Type: SubjectUser, ID: "user-2"}

		// Two answers for the subject, one of them cleared — an archived row
		// that still says what they chose, which is what an erasure has to
		// reach and a clearance does not.
		_, err := store.SetValue(t.Context(), testScope, testSubject, "digest", "daily")
		must.NoError(t, err)
		_, err = store.SetValue(t.Context(), testScope, testSubject, "compact", "true")
		must.NoError(t, err)
		must.NoError(t, store.ClearValue(t.Context(), testScope, testSubject, "compact"))

		// And two rows the erasure must not touch: another subject's in this
		// scope, and this subject's in another.
		_, err = store.SetValue(t.Context(), testScope, second, "digest", "never")
		must.NoError(t, err)
		_, err = store.SetValue(t.Context(), otherScope, testSubject, "digest", "weekly")
		must.NoError(t, err)

		test.EqOp(t, int64(2), erase(t, store, testScope, testSubject))

		// Gone rather than archived: a page that asks for archived rows too
		// finds nothing.
		everything := filtering.DefaultQueryFilter()
		everything.IncludeArchived = pointer.To(true)

		mine, err := store.ListValuesForSubject(t.Context(), testScope, testSubject, everything)
		must.NoError(t, err)
		test.SliceEmpty(t, mine.Data)

		_, err = store.GetValue(t.Context(), testScope, testSubject, "digest")
		test.ErrorIs(t, err, ErrValueNotFound)

		theirs, err := store.GetValue(t.Context(), testScope, second, "digest")
		must.NoError(t, err)
		test.EqOp(t, "never", theirs.Raw)

		elsewhere, err := store.GetValue(t.Context(), otherScope, testSubject, "digest")
		must.NoError(t, err)
		test.EqOp(t, "weekly", elsewhere.Raw)

		// A second erasure has nothing to remove, and says so with a count
		// rather than an error.
		test.EqOp(t, int64(0), erase(t, store, testScope, testSubject))

		// The definitions are untouched: an erasure is about a person, not the
		// catalog, and the subject can answer again.
		revived, err := store.SetValue(t.Context(), testScope, testSubject, "digest", "weekly")
		must.NoError(t, err)
		test.EqOp(t, "weekly", revived.Raw)
	})

	t.Run("an erasure rolls back with the transaction it ran in", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		mustCreate(t, store, testScope, stringDefinition("digest"))

		_, err := store.SetValue(t.Context(), testScope, testSubject, "digest", "daily")
		must.NoError(t, err)

		err = store.client.WithTransaction(t.Context(), func(tx database.Tx) error {
			deleted, txErr := store.DeleteValuesForSubject(t.Context(), tx, testScope, testSubject)
			if txErr != nil {
				return txErr
			}

			test.EqOp(t, int64(1), deleted)

			return errCompanionWrite
		})
		must.ErrorIs(t, err, errCompanionWrite)

		read, err := store.GetValue(t.Context(), testScope, testSubject, "digest")
		must.NoError(t, err)
		test.EqOp(t, "daily", read.Raw)
	})

	t.Run("an erasure has to name somebody", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		var (
			unset                      tenancy.Scope
			unscoped, untyped, unnamed error
		)

		must.NoError(t, store.client.WithTransaction(t.Context(), func(tx database.Tx) error {
			_, unscoped = store.DeleteValuesForSubject(t.Context(), tx, unset, testSubject)
			_, untyped = store.DeleteValuesForSubject(t.Context(), tx, testScope, Subject{ID: "user-1"})
			_, unnamed = store.DeleteValuesForSubject(t.Context(), tx, testScope, Subject{Type: SubjectUser})

			return nil
		}))

		test.ErrorIs(t, unscoped, tenancy.ErrNoScope)
		test.ErrorIs(t, untyped, ErrEmptySubjectType)
		test.ErrorIs(t, unnamed, ErrEmptySubjectID)
	})
}

// erase runs DeleteValuesForSubject in its own transaction and returns the
// count, since every caller of it here wants exactly that.
func erase(t *testing.T, store *SQLStore, scope tenancy.Scope, subject Subject) int64 {
	t.Helper()

	var deleted int64

	must.NoError(t, store.client.WithTransaction(t.Context(), func(tx database.Tx) error {
		var err error
		deleted, err = store.DeleteValuesForSubject(t.Context(), tx, scope, subject)

		return err
	}))

	return deleted
}
