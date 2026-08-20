package resources_test

import (
	"database/sql"
	"testing"
	"time"

	"github.com/primandproper/platform-go/v12/database/dialect"
	"github.com/primandproper/platform-go/v12/resources"
	"github.com/primandproper/platform-go/v12/tenancy"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// Draft is a row only its author may see: the OwnerReadsAndWrites case, which
// the comments declaration does not exercise because a comment is public to
// whoever can see the thing it is on.
type Draft struct {
	CreatedAt     time.Time
	LastUpdatedAt *time.Time
	ArchivedAt    *time.Time
	ID            string
	Body          string
	Topic         string
	BelongsToUser string
	Scope         tenancy.Scope
}

func draftsDefinition() resources.Definition[Draft] {
	return resources.Definition[Draft]{
		Name:  "draft",
		Table: "drafts",
		Columns: []resources.Column[Draft]{
			resources.ID(func(d *Draft) *string { return &d.ID }),
			resources.Scope("scope", func(d *Draft) *tenancy.Scope { return &d.Scope }),
			resources.Field("topic", func(d *Draft) *string { return &d.Topic }).Immutable(),
			resources.Field("body", func(d *Draft) *string { return &d.Body }),
			resources.Owner("belongs_to_user", func(d *Draft) *string { return &d.BelongsToUser }, resources.OwnerReadsAndWrites),
			resources.Field("created_at", func(d *Draft) *time.Time { return &d.CreatedAt }),
			resources.Field("last_updated_at", func(d *Draft) **time.Time { return &d.LastUpdatedAt }),
			resources.Field("archived_at", func(d *Draft) **time.Time { return &d.ArchivedAt }),
		},
		Lookups: []resources.Lookup{resources.On("topic")},
	}
}

const draftsSchema = `
CREATE TABLE IF NOT EXISTS drafts (
    id TEXT NOT NULL PRIMARY KEY,
    scope TEXT NOT NULL,
    topic TEXT NOT NULL,
    body TEXT NOT NULL DEFAULT '',
    belongs_to_user TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_updated_at DATETIME,
    archived_at DATETIME
);`

func newDraftStore(t *testing.T) *resources.Store[Draft] {
	t.Helper()

	client := newSQLiteClient(t)
	mustExec(t, client, draftsSchema)

	resource, err := resources.Define(dialect.SQLite, draftsDefinition())
	must.NoError(t, err)

	store, err := resources.NewStore(resource, client)
	must.NoError(t, err)

	return store
}

// draft builds one of alice's drafts in the acme account.
func draft(topic, body string) *Draft {
	return &Draft{Topic: topic, Body: body, BelongsToUser: "user_alice"}
}

func acme() tenancy.Scope { return tenancy.Of("account_acme") }

func TestStore_OwnerReadsAndWrites(T *testing.T) {
	T.Parallel()

	T.Run("only the owner may read", func(t *testing.T) {
		t.Parallel()

		store := newDraftStore(t)
		ctx := t.Context()

		created, err := store.Create(ctx, acme(), alice(), draft("dinner", "not ready yet"))
		must.NoError(t, err)

		found, err := store.Get(ctx, acme(), alice(), created.ID)
		must.NoError(t, err)
		test.EqOp(t, "not ready yet", found.Body)

		// Bob cannot see it at all, which is what this gate means beyond
		// OwnerWrites: not "may not edit" but "is not among the rows".
		_, err = store.Get(ctx, acme(), bob(), created.ID)
		test.ErrorIs(t, err, sql.ErrNoRows)

		exists, err := store.Exists(ctx, acme(), bob(), created.ID)
		must.NoError(t, err)
		test.False(t, exists)

		rows, err := store.GetMany(ctx, acme(), bob(), created.ID)
		must.NoError(t, err)
		test.SliceEmpty(t, rows)

		page, err := store.List(ctx, acme(), bob(), nil, resources.By("topic", "dinner"))
		must.NoError(t, err)
		test.SliceLen(t, 0, page.Data)
	})

	T.Run("the system actor reads across owners", func(t *testing.T) {
		t.Parallel()

		store := newDraftStore(t)
		ctx := t.Context()

		mine, err := store.Create(ctx, acme(), alice(), draft("dinner", "mine"))
		must.NoError(t, err)

		theirs := draft("dinner", "bob's")
		theirs.BelongsToUser = "user_bob"

		stored, err := store.Create(ctx, acme(), bob(), theirs)
		must.NoError(t, err)

		// A retention reaper, a data-privacy export, a cascade: all of them need
		// every author's rows, and none of them has an author of its own. This
		// is the read that used to have no statement to issue.
		page, err := store.List(ctx, acme(), resources.System(), nil, resources.By("topic", "dinner"))
		must.NoError(t, err)
		test.SliceLen(t, 2, page.Data)

		found, err := store.Get(ctx, acme(), resources.System(), stored.ID)
		must.NoError(t, err)
		test.EqOp(t, "bob's", found.Body)

		rows, err := store.GetMany(ctx, acme(), resources.System(), mine.ID, stored.ID)
		must.NoError(t, err)
		test.SliceLen(t, 2, rows)
	})

	T.Run("a cascade archives every owner's rows", func(t *testing.T) {
		t.Parallel()

		store := newDraftStore(t)
		ctx := t.Context()

		theirs := draft("breakfast", "bob's")
		theirs.BelongsToUser = "user_bob"

		_, err := store.Create(ctx, acme(), alice(), draft("breakfast", "mine"))
		must.NoError(t, err)

		_, err = store.Create(ctx, acme(), bob(), theirs)
		must.NoError(t, err)

		must.NoError(t, store.ArchiveMatching(ctx, acme(), resources.System(), resources.By("topic", "breakfast")))

		page, err := store.List(ctx, acme(), resources.System(), nil, resources.By("topic", "breakfast"))
		must.NoError(t, err)
		test.SliceLen(t, 0, page.Data)
	})

	T.Run("the scope is written from the call rather than trusted from the row", func(t *testing.T) {
		t.Parallel()

		store := newDraftStore(t)
		ctx := t.Context()

		claiming := draft("supper", "written into somebody else's account")
		claiming.Scope = tenancy.Of("account_someone_else")

		created, err := store.Create(ctx, acme(), alice(), claiming)
		must.NoError(t, err)
		test.EqOp(t, "account_acme", created.Scope.Owner())

		// And the account it named cannot see it.
		page, err := store.List(ctx, tenancy.Of("account_someone_else"), resources.System(), nil, resources.By("topic", "supper"))
		must.NoError(t, err)
		test.SliceLen(t, 0, page.Data)
	})

	T.Run("a scoped resource keeps one tenant's rows out of another's reads", func(t *testing.T) {
		t.Parallel()

		store := newDraftStore(t)
		ctx := t.Context()

		_, err := store.Create(ctx, acme(), alice(), draft("brunch", "acme's"))
		must.NoError(t, err)

		other := tenancy.Of("account_other")

		_, err = store.Create(ctx, other, alice(), draft("brunch", "the other account's"))
		must.NoError(t, err)

		page, err := store.List(ctx, acme(), alice(), nil, resources.By("topic", "brunch"))
		must.NoError(t, err)
		must.SliceLen(t, 1, page.Data)
		test.EqOp(t, "acme's", page.Data[0].Body)

		// Reading the global scope of a table whose rows all name a tenant finds
		// nothing, rather than everything.
		page, err = store.List(ctx, tenancy.Global(), alice(), nil, resources.By("topic", "brunch"))
		must.NoError(t, err)
		test.SliceLen(t, 0, page.Data)
	})

	T.Run("a create whose row names another author is still readable by its author", func(t *testing.T) {
		t.Parallel()

		store := newDraftStore(t)
		ctx := t.Context()

		// The store re-reads a create inside its transaction, and that re-read is
		// its own machinery rather than a consumer read — so it is not keyed on
		// the owner, and a row written on somebody's behalf comes back rather
		// than failing as "no such row" from the statement that just wrote it.
		theirs := draft("late night", "for bob")
		theirs.BelongsToUser = "user_bob"

		created, err := store.Create(ctx, acme(), alice(), theirs)
		must.NoError(t, err)
		test.EqOp(t, "user_bob", created.BelongsToUser)

		found, err := store.Get(ctx, acme(), bob(), created.ID)
		must.NoError(t, err)
		test.EqOp(t, "for bob", found.Body)
	})
}
