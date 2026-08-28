package registry

import (
	"testing"

	"github.com/primandproper/platform-go/v13/database/dialect"
	databasemock "github.com/primandproper/platform-go/v13/database/mock"
	platformerrors "github.com/primandproper/platform-go/v13/errors"
	"github.com/primandproper/platform-go/v13/filtering"
	"github.com/primandproper/platform-go/v13/identifiers"
	"github.com/primandproper/platform-go/v13/tenancy"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestNewSQLStore(T *testing.T) {
	T.Parallel()

	newClient := func(d dialect.Dialect) *databasemock.ClientMock {
		return &databasemock.ClientMock{DialectFunc: func() dialect.Dialect { return d }}
	}

	T.Run("builds against every dialect the querier was generated for", func(t *testing.T) {
		t.Parallel()

		for _, d := range []dialect.Dialect{dialect.Postgres, dialect.MySQL, dialect.SQLite} {
			store, err := NewSQLStore(newClient(d))
			must.NoError(t, err, must.Sprintf("dialect %s", d))
			must.NotNil(t, store)
		}
	})

	T.Run("refuses a nil client", func(t *testing.T) {
		t.Parallel()

		store, err := NewSQLStore(nil)
		must.ErrorIs(t, err, ErrNilDatabaseClient)
		must.ErrorIs(t, err, platformerrors.ErrNilInputParameter)
		test.Nil(t, store)
	})

	T.Run("refuses a dialect it has no statements for", func(t *testing.T) {
		t.Parallel()

		store, err := NewSQLStore(newClient(dialect.Dialect("oracle")))
		must.Error(t, err)
		test.Nil(t, store)
	})

	T.Run("refuses a prefix that cannot render", func(t *testing.T) {
		t.Parallel()

		// Vetted against the identifiers it actually produces, so a prefix that
		// is legal alone and yields an over-long index name fails at
		// construction rather than at the first query.
		store, err := NewSQLStore(newClient(dialect.Postgres), WithTablePrefix("has space"))
		must.Error(t, err)
		test.Nil(t, store)
	})

	T.Run("ignores a nil option", func(t *testing.T) {
		t.Parallel()

		store, err := NewSQLStore(newClient(dialect.Postgres), nil)
		must.NoError(t, err)
		must.NotNil(t, store)
	})
}

// TestSQLStore_SQLite runs the whole behavioral contract against SQLite, which
// executes the real generated SQL without a container. The same suite runs
// against Postgres and MySQL in containers_test.go.
func TestSQLStore_SQLite(T *testing.T) {
	T.Parallel()

	runStoreSuite(T, newSQLiteEnv(T))
}

// runStoreSuite is the whole behavioral contract, run once per dialect.
//
// Each subtest migrates its own prefixed table, because a scope's object list is
// global within the table: without that, one subtest's rows are another's page.
func runStoreSuite(t *testing.T, env *storeEnv) {
	t.Helper()

	t.Run("records an object and reads it back", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		object := newObject(testScope, "avatars/grace/original.png", "user_1")
		must.NoError(t, store.RecordObject(t.Context(), object))

		// The id is minted by the write, and the creation time comes back from
		// the row rather than being left at the zero time.
		test.NotEq(t, "", object.ID)
		test.False(t, object.CreatedAt.IsZero())
		test.Nil(t, object.LastUpdatedAt)
		test.Nil(t, object.ArchivedAt)

		read, err := store.GetObject(t.Context(), testScope, object.ID)
		must.NoError(t, err)
		test.EqOp(t, object.ID, read.ID)
		test.EqOp(t, object.Key, read.Key)
		test.EqOp(t, "image/png", read.ContentType)
		test.EqOp(t, int64(1024), read.Size)
		test.EqOp(t, "user_1", read.OwnerID)
		test.EqOp(t, testScope, read.Scope)
		test.EqOp(t, object.CreatedAt.UTC(), read.CreatedAt)
	})

	t.Run("keeps a caller-supplied id", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		// The reference to the upload is often minted before the upload, so the
		// row a consumer is writing in the same request can name it.
		id := identifiers.New()

		object := newObject(testScope, "receipts/"+id+".pdf", "user_1")
		object.ID = id

		must.NoError(t, store.RecordObject(t.Context(), object))
		test.EqOp(t, id, object.ID)

		read, err := store.GetObject(t.Context(), testScope, id)
		must.NoError(t, err)
		test.EqOp(t, id, read.ID)
	})

	t.Run("records what an object belongs to", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		object := newObject(testScope, "receipts/january.pdf", "user_1")
		object.BelongsTo = Subject{Type: "invoice", ID: "invoice_1"}

		must.NoError(t, store.RecordObject(t.Context(), object))

		read, err := store.GetObject(t.Context(), testScope, object.ID)
		must.NoError(t, err)
		test.EqOp(t, "invoice", read.BelongsTo.Type)
		test.EqOp(t, "invoice_1", read.BelongsTo.ID)
		test.True(t, read.BelongsTo.Attached())
	})

	t.Run("refuses a key already registered in the scope", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		key := "avatars/grace/original.png"
		must.NoError(t, store.RecordObject(t.Context(), newObject(testScope, key, "user_1")))

		err := store.RecordObject(t.Context(), newObject(testScope, key, "user_2"))
		must.ErrorIs(t, err, ErrObjectKeyTaken)

		// The same key in another tenant is another object, and registering it
		// is not a collision.
		must.NoError(t, store.RecordObject(t.Context(), newObject(otherScope, key, "user_3")))
	})

	t.Run("keeps a key taken after the row is archived", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		key := "receipts/january.pdf"

		object := newObject(testScope, key, "user_1")
		must.NoError(t, store.RecordObject(t.Context(), object))
		must.NoError(t, store.ArchiveObject(t.Context(), testScope, object.ID))

		// Archival is metadata-only and the bytes are still in the bucket, so a
		// second row for the same key would be two rows describing one object.
		must.ErrorIs(t, store.RecordObject(t.Context(), newObject(testScope, key, "user_1")), ErrObjectKeyTaken)
	})

	t.Run("refuses a row that could not answer who may read it", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		must.ErrorIs(t, store.RecordObject(t.Context(), nil), ErrNilObject)

		noKey := newObject(testScope, "", "user_1")
		must.Error(t, store.RecordObject(t.Context(), noKey))

		noOwner := newObject(testScope, "avatars/nobody.png", "")
		must.Error(t, store.RecordObject(t.Context(), noOwner))

		halfSubject := newObject(testScope, "avatars/half.png", "user_1")
		halfSubject.BelongsTo = Subject{Type: "invoice"}
		must.ErrorIs(t, store.RecordObject(t.Context(), halfSubject), ErrPartialSubject)
	})

	t.Run("reads by the key the bytes live at", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		key := "avatars/grace/original.png"
		object := newObject(testScope, key, "user_1")
		must.NoError(t, store.RecordObject(t.Context(), object))

		read, err := store.GetObjectByKey(t.Context(), testScope, key)
		must.NoError(t, err)
		test.EqOp(t, object.ID, read.ID)
		test.EqOp(t, "user_1", read.OwnerID)

		// The read a request runs is scoped, so it is not an oracle for which
		// keys exist in another tenant.
		_, err = store.GetObjectByKey(t.Context(), otherScope, key)
		must.ErrorIs(t, err, ErrObjectNotFound)

		_, err = store.GetObjectByKey(t.Context(), testScope, "avatars/nobody.png")
		must.ErrorIs(t, err, ErrObjectNotFound)
	})

	t.Run("answers a read from another scope as absent", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		object := newObject(testScope, "avatars/grace/original.png", "user_1")
		must.NoError(t, store.RecordObject(t.Context(), object))

		_, err := store.GetObject(t.Context(), otherScope, object.ID)
		must.ErrorIs(t, err, ErrObjectNotFound)

		_, err = store.GetObject(t.Context(), testScope, identifiers.New())
		must.ErrorIs(t, err, ErrObjectNotFound)
	})

	t.Run("archives metadata only, once", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		object := newObject(testScope, "receipts/january.pdf", "user_1")
		must.NoError(t, store.RecordObject(t.Context(), object))

		// Another tenant cannot archive it, and the refusal reads as absence
		// rather than as a permission error, which is the answer that does not
		// confirm the row exists.
		must.ErrorIs(t, store.ArchiveObject(t.Context(), otherScope, object.ID), ErrObjectNotFound)

		must.NoError(t, store.ArchiveObject(t.Context(), testScope, object.ID))

		// The statement carries the archived predicate, so a second archive
		// touches nothing rather than moving the timestamp forward.
		must.ErrorIs(t, store.ArchiveObject(t.Context(), testScope, object.ID), ErrObjectNotFound)

		_, err := store.GetObject(t.Context(), testScope, object.ID)
		must.ErrorIs(t, err, ErrObjectNotFound)
	})

	t.Run("pages the scope's objects in the direction the filter names", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		first := newObject(testScope, "a.png", "user_1")
		first.ID = "obj_1"
		must.NoError(t, store.RecordObject(t.Context(), first))

		second := newObject(testScope, "b.png", "user_1")
		second.ID = "obj_2"
		must.NoError(t, store.RecordObject(t.Context(), second))

		// A neighbor's row must never appear in this tenant's page.
		must.NoError(t, store.RecordObject(t.Context(), newObject(otherScope, "c.png", "user_9")))

		page, err := store.ListObjects(t.Context(), testScope, nil)
		must.NoError(t, err)
		must.SliceLen(t, 2, page.Data)
		test.EqOp(t, "obj_1", page.Data[0].ID)
		test.EqOp(t, "obj_2", page.Data[1].ID)

		filtered, total, known := page.Counts()
		must.True(t, known)
		test.EqOp(t, uint64(2), filtered)
		test.EqOp(t, uint64(2), total)

		newestFirst, err := store.ListObjects(t.Context(), testScope,
			&filtering.QueryFilter{SortBy: filtering.SortDescending})
		must.NoError(t, err)
		must.SliceLen(t, 2, newestFirst.Data)
		test.EqOp(t, "obj_2", newestFirst.Data[0].ID)
		test.EqOp(t, "obj_1", newestFirst.Data[1].ID)
	})

	t.Run("walks a page at a time", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		for _, id := range []string{"obj_1", "obj_2", "obj_3"} {
			object := newObject(testScope, id+".png", "user_1")
			object.ID = id
			must.NoError(t, store.RecordObject(t.Context(), object))
		}

		size := uint16(2)

		page, err := store.ListObjects(t.Context(), testScope, &filtering.QueryFilter{MaxResponseSize: &size})
		must.NoError(t, err)
		must.SliceLen(t, 2, page.Data)
		must.EqOp(t, "obj_2", page.Cursor)

		next, err := store.ListObjects(t.Context(), testScope,
			&filtering.QueryFilter{MaxResponseSize: &size, Cursor: &page.Cursor})
		must.NoError(t, err)
		must.SliceLen(t, 1, next.Data)
		test.EqOp(t, "obj_3", next.Data[0].ID)

		// The counts describe the collection rather than the page, so they do
		// not shrink as the caller walks it.
		_, total, known := next.Counts()
		must.True(t, known)
		test.EqOp(t, uint64(3), total)
	})

	t.Run("leaves archived rows out unless asked", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		object := newObject(testScope, "a.png", "user_1")
		must.NoError(t, store.RecordObject(t.Context(), object))
		must.NoError(t, store.RecordObject(t.Context(), newObject(testScope, "b.png", "user_1")))
		must.NoError(t, store.ArchiveObject(t.Context(), testScope, object.ID))

		live, err := store.ListObjects(t.Context(), testScope, nil)
		must.NoError(t, err)
		must.SliceLen(t, 1, live.Data)

		includeArchived := true

		all, err := store.ListObjects(t.Context(), testScope,
			&filtering.QueryFilter{IncludeArchived: &includeArchived})
		must.NoError(t, err)
		must.SliceLen(t, 2, all.Data)

		for _, o := range all.Data {
			if o.ID == object.ID {
				test.NotNil(t, o.ArchivedAt)
			}
		}
	})

	t.Run("pages one owner's objects", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		mine := newObject(testScope, "a.png", "user_1")
		must.NoError(t, store.RecordObject(t.Context(), mine))
		must.NoError(t, store.RecordObject(t.Context(), newObject(testScope, "b.png", "user_2")))
		must.NoError(t, store.RecordObject(t.Context(), newObject(otherScope, "c.png", "user_1")))

		page, err := store.ListObjectsByOwner(t.Context(), testScope, "user_1", nil)
		must.NoError(t, err)
		must.SliceLen(t, 1, page.Data)
		test.EqOp(t, mine.ID, page.Data[0].ID)

		// The neighbor's object belongs to a user with the same id, and the
		// scope is what keeps it out.
		neighbors, err := store.ListObjectsByOwner(t.Context(), otherScope, "user_1", nil)
		must.NoError(t, err)
		must.SliceLen(t, 1, neighbors.Data)
		test.EqOp(t, "c.png", neighbors.Data[0].Key)

		empty, err := store.ListObjectsByOwner(t.Context(), testScope, "user_9", nil)
		must.NoError(t, err)
		test.SliceEmpty(t, empty.Data)
	})

	t.Run("pages what is attached to one thing", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		invoice := newObject(testScope, "receipts/january.pdf", "user_1")
		invoice.BelongsTo = Subject{Type: "invoice", ID: "invoice_1"}
		must.NoError(t, store.RecordObject(t.Context(), invoice))

		// Same id, different type: an id without its type names something else
		// in another one of the consumer's tables.
		ticket := newObject(testScope, "tickets/screenshot.png", "user_1")
		ticket.BelongsTo = Subject{Type: "ticket", ID: "invoice_1"}
		must.NoError(t, store.RecordObject(t.Context(), ticket))

		// And a standalone upload, attached to nothing.
		must.NoError(t, store.RecordObject(t.Context(), newObject(testScope, "loose.png", "user_1")))

		page, err := store.ListObjectsBySubject(t.Context(), testScope, Subject{Type: "invoice", ID: "invoice_1"}, nil)
		must.NoError(t, err)
		must.SliceLen(t, 1, page.Data)
		test.EqOp(t, invoice.ID, page.Data[0].ID)

		// The zero subject is refused rather than bound: every standalone
		// upload carries the empty pair, so the statement would report them as
		// one thing's attachments.
		_, err = store.ListObjectsBySubject(t.Context(), testScope, Subject{}, nil)
		must.ErrorIs(t, err, ErrUnattachedSubject)
	})

	t.Run("refuses an unset scope on every method", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		var unset tenancy.Scope

		_, err := store.GetObject(t.Context(), unset, "obj_1")
		must.Error(t, err)

		_, err = store.GetObjectByKey(t.Context(), unset, "a.png")
		must.Error(t, err)

		_, err = store.ListObjects(t.Context(), unset, nil)
		must.Error(t, err)

		_, err = store.ListObjectsByOwner(t.Context(), unset, "user_1", nil)
		must.Error(t, err)

		_, err = store.ListObjectsBySubject(t.Context(), unset, Subject{Type: "invoice", ID: "invoice_1"}, nil)
		must.Error(t, err)

		must.Error(t, store.ArchiveObject(t.Context(), unset, "obj_1"))

		// And on the write, where the scope rides on the row rather than being
		// a parameter.
		must.Error(t, store.RecordObject(t.Context(), newObject(unset, "a.png", "user_1")))
	})

	t.Run("stores the global scope as itself", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		// A single-tenant application passes tenancy.Global() everywhere and
		// gets exactly what it would have had without the column — including
		// the fact that the global scope matches only itself.
		object := newObject(tenancy.Global(), "a.png", "user_1")
		must.NoError(t, store.RecordObject(t.Context(), object))

		read, err := store.GetObject(t.Context(), tenancy.Global(), object.ID)
		must.NoError(t, err)
		test.True(t, read.Scope.IsGlobal())

		_, err = store.GetObject(t.Context(), testScope, object.ID)
		must.ErrorIs(t, err, ErrObjectNotFound)
	})
}
