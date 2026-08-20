package resources_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/primandproper/platform-go/v12/database"
	"github.com/primandproper/platform-go/v12/database/dialect"
	platformerrors "github.com/primandproper/platform-go/v12/errors"
	"github.com/primandproper/platform-go/v12/filtering"
	"github.com/primandproper/platform-go/v12/resources"
	"github.com/primandproper/platform-go/v12/tenancy"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestStore_CreateAndRead(T *testing.T) {
	T.Parallel()

	T.Run("create reads back what the server stored", func(t *testing.T) {
		t.Parallel()

		store, _ := newCommentStore(t)
		ctx := t.Context()

		created, err := store.Create(ctx, tenancy.Global(), alice(), &Comment{
			Content:       "the roux needs longer",
			TargetType:    "recipes",
			ReferencedID:  "recipe_1",
			BelongsToUser: "user_alice",
		})
		must.NoError(t, err)
		must.NotNil(t, created)

		test.NotEq(t, "", created.ID)
		test.EqOp(t, "the roux needs longer", created.Content)
		// created_at came from the server, not from this process.
		test.False(t, created.CreatedAt.IsZero())
		test.Nil(t, created.LastUpdatedAt)
		test.Nil(t, created.ArchivedAt)
		test.Nil(t, created.ParentCommentID)

		// OwnerWrites: bob can read alice's comment.
		fetched, err := store.Get(ctx, tenancy.Global(), bob(), created.ID)
		must.NoError(t, err)
		test.EqOp(t, created.ID, fetched.ID)
		test.EqOp(t, created.Content, fetched.Content)

		exists, err := store.Exists(ctx, tenancy.Global(), bob(), created.ID)
		must.NoError(t, err)
		test.True(t, exists)
	})

	T.Run("a nullable column round-trips as a pointer", func(t *testing.T) {
		t.Parallel()

		store, _ := newCommentStore(t)
		ctx := t.Context()

		parent, err := store.Create(ctx, tenancy.Global(), alice(), comment("parent", "recipe_nullable", "user_alice"))
		must.NoError(t, err)

		reply := comment("reply", "recipe_nullable", "user_bob")
		reply.ParentCommentID = &parent.ID

		stored, err := store.Create(ctx, tenancy.Global(), bob(), reply)
		must.NoError(t, err)
		must.NotNil(t, stored.ParentCommentID)
		test.EqOp(t, parent.ID, *stored.ParentCommentID)
	})

	T.Run("a supplied id is kept rather than replaced", func(t *testing.T) {
		t.Parallel()

		store, _ := newCommentStore(t)

		row := comment("named", "recipe_named", "user_alice")
		row.ID = "comment_of_my_own_choosing"

		created, err := store.Create(t.Context(), tenancy.Global(), alice(), row)
		must.NoError(t, err)
		test.EqOp(t, "comment_of_my_own_choosing", created.ID)
	})

	T.Run("an id factory the caller supplied is the one used", func(t *testing.T) {
		t.Parallel()

		store, _ := newCommentStore(t, resources.WithIDFactory(func() string { return "minted" }))

		created, err := store.Create(t.Context(), tenancy.Global(), alice(), comment("a", "recipe_minted", "user_alice"))
		must.NoError(t, err)
		test.EqOp(t, "minted", created.ID)
	})
}

func TestStore_Update(T *testing.T) {
	T.Parallel()

	T.Run("only the owner may update", func(t *testing.T) {
		t.Parallel()

		store, _ := newCommentStore(t)
		ctx := t.Context()

		created, err := store.Create(ctx, tenancy.Global(), alice(), comment("original", "recipe_owner", "user_alice"))
		must.NoError(t, err)

		created.Content = "edited by bob"
		_, err = store.Update(ctx, tenancy.Global(), bob(), created)
		test.ErrorIs(t, err, resources.ErrNoRowsAffected)

		created.Content = "edited by alice"
		updated, err := store.Update(ctx, tenancy.Global(), alice(), created)
		must.NoError(t, err)
		test.EqOp(t, "edited by alice", updated.Content)
		must.NotNil(t, updated.LastUpdatedAt)
	})

	T.Run("an update cannot move a comment to another reference", func(t *testing.T) {
		t.Parallel()

		store, _ := newCommentStore(t)
		ctx := t.Context()

		created, err := store.Create(ctx, tenancy.Global(), alice(), comment("immutable", "recipe_immutable", "user_alice"))
		must.NoError(t, err)

		// Both of these are Immutable, so the update must ignore them.
		created.ReferencedID = "recipe_somewhere_else"
		created.BelongsToUser = "user_bob"
		created.Content = "still alice's, still here"

		updated, err := store.Update(ctx, tenancy.Global(), alice(), created)
		must.NoError(t, err)
		test.EqOp(t, "recipe_immutable", updated.ReferencedID)
		test.EqOp(t, "user_alice", updated.BelongsToUser)
		test.EqOp(t, "still alice's, still here", updated.Content)
	})

	T.Run("the system actor updates any owner's row", func(t *testing.T) {
		t.Parallel()

		store, _ := newCommentStore(t)
		ctx := t.Context()

		created, err := store.Create(ctx, tenancy.Global(), alice(), comment("original", "recipe_system", "user_alice"))
		must.NoError(t, err)

		created.Content = "redacted by the platform"
		updated, err := store.Update(ctx, tenancy.Global(), resources.System(), created)
		must.NoError(t, err)
		test.EqOp(t, "redacted by the platform", updated.Content)
	})
}

func TestStore_Archive(T *testing.T) {
	T.Parallel()

	T.Run("archive is a soft delete the reads respect", func(t *testing.T) {
		t.Parallel()

		store, _ := newCommentStore(t)
		ctx := t.Context()

		created, err := store.Create(ctx, tenancy.Global(), alice(), comment("doomed", "recipe_archive", "user_alice"))
		must.NoError(t, err)

		test.ErrorIs(t, store.Archive(ctx, tenancy.Global(), bob(), created.ID), resources.ErrNoRowsAffected)
		must.NoError(t, store.Archive(ctx, tenancy.Global(), alice(), created.ID))

		_, err = store.Get(ctx, tenancy.Global(), alice(), created.ID)
		test.ErrorIs(t, err, sql.ErrNoRows)

		// A second archive matches nothing, because the first one already set
		// archived_at — and it reports that rather than the pre-read's own
		// "no such row", which is a different sentence about the same state.
		test.ErrorIs(t, store.Archive(ctx, tenancy.Global(), alice(), created.ID), resources.ErrNoRowsAffected)
	})

	T.Run("a cascade archives every author's rows and reports each one", func(t *testing.T) {
		t.Parallel()

		store, seen := newCommentStore(t)
		ctx := t.Context()

		for _, c := range []*Comment{
			comment("x", "recipe_cascade", "user_alice"),
			comment("y", "recipe_cascade", "user_bob"),
		} {
			_, err := store.Create(ctx, tenancy.Global(), alice(), c)
			must.NoError(t, err)
		}

		mark := seen.mark()

		matches := []resources.Match{
			resources.By("target_type", "recipes"),
			resources.By("referenced_id", "recipe_cascade"),
		}

		// A cascade crosses owners, so a user's actor may not perform one.
		test.ErrorIs(t, store.ArchiveMatching(ctx, tenancy.Global(), alice(), matches...), platformerrors.ErrPermissionDenied)
		must.NoError(t, store.ArchiveMatching(ctx, tenancy.Global(), resources.System(), matches...))

		page, err := store.List(ctx, tenancy.Global(), alice(), nil, matches...)
		must.NoError(t, err)
		test.SliceLen(t, 0, page.Data)

		// One change per row, not one for the statement.
		test.SliceLen(t, 2, seen.since(mark))

		for _, change := range seen.since(mark) {
			test.EqOp(t, resources.OpArchived, change.Op)
			must.NotNil(t, change.Row)
		}
	})

	T.Run("a cascade needs matches rather than archiving the table", func(t *testing.T) {
		t.Parallel()

		store, _ := newCommentStore(t)

		err := store.ArchiveMatching(t.Context(), tenancy.Global(), resources.System())
		test.ErrorIs(t, err, resources.ErrUndeclaredLookup)
	})
}

func TestStore_List(T *testing.T) {
	T.Parallel()

	T.Run("the reference lookup returns every author's comments", func(t *testing.T) {
		t.Parallel()

		store, _ := newCommentStore(t)
		ctx := t.Context()

		for _, c := range []*Comment{
			comment("a", "recipe_list", "user_alice"),
			comment("b", "recipe_list", "user_bob"),
			issueComment("c", "recipe_list", "user_bob"),
		} {
			_, err := store.Create(ctx, tenancy.Global(), alice(), c)
			must.NoError(t, err)
		}

		page, err := store.List(ctx, tenancy.Global(), alice(), nil,
			resources.By("target_type", "recipes"),
			resources.By("referenced_id", "recipe_list"),
		)
		must.NoError(t, err)
		test.SliceLen(t, 2, page.Data)
		test.EqOp(t, uint64(2), page.FilteredCount)
		// The other target_type is a different reference, not a filtered-out row
		// of this one.
		test.EqOp(t, uint64(2), page.TotalCount)
	})

	T.Run("the match set is a set rather than a sequence", func(t *testing.T) {
		t.Parallel()

		store, _ := newCommentStore(t)
		ctx := t.Context()

		_, err := store.Create(ctx, tenancy.Global(), alice(), comment("a", "recipe_order", "user_alice"))
		must.NoError(t, err)

		// The declaration says On("target_type", "referenced_id"); this names
		// them the other way round and is the same read.
		page, err := store.List(ctx, tenancy.Global(), alice(), nil,
			resources.By("referenced_id", "recipe_order"),
			resources.By("target_type", "recipes"),
		)
		must.NoError(t, err)
		test.SliceLen(t, 1, page.Data)
	})

	T.Run("an undeclared match set is refused rather than scanned", func(t *testing.T) {
		t.Parallel()

		store, _ := newCommentStore(t)

		_, err := store.List(t.Context(), tenancy.Global(), alice(), nil, resources.By("content", "a"))
		test.ErrorIs(t, err, resources.ErrUndeclaredLookup)
	})

	T.Run("a match on a column the resource does not have is refused", func(t *testing.T) {
		t.Parallel()

		store, _ := newCommentStore(t)

		_, err := store.List(t.Context(), tenancy.Global(), alice(), nil, resources.By("nonexistent", "a"))
		test.ErrorIs(t, err, resources.ErrUnknownColumn)
	})

	T.Run("a match whose value cannot be the column's is refused before the driver", func(t *testing.T) {
		t.Parallel()

		store, _ := newCommentStore(t)

		_, err := store.List(t.Context(), tenancy.Global(), alice(), nil,
			resources.By("target_type", "recipes"),
			resources.By("referenced_id", 12),
		)
		test.ErrorIs(t, err, resources.ErrMatchTypeMismatch)
	})

	T.Run("the page walks by cursor", func(t *testing.T) {
		t.Parallel()

		store, _ := newCommentStore(t)
		ctx := t.Context()

		for i := range 5 {
			_, err := store.Create(ctx, tenancy.Global(), alice(), comment(string(rune('a'+i)), "recipe_paging", "user_alice"))
			must.NoError(t, err)
		}

		matches := []resources.Match{
			resources.By("target_type", "recipes"),
			resources.By("referenced_id", "recipe_paging"),
		}

		size := uint16(2)
		filter := filtering.DefaultQueryFilter()
		filter.MaxResponseSize = &size

		var walked int

		for range 5 {
			page, err := store.List(ctx, tenancy.Global(), alice(), filter, matches...)
			must.NoError(t, err)

			if len(page.Data) == 0 {
				break
			}

			walked += len(page.Data)

			// The counts ride on the rows — they are subqueries in the SELECT
			// list — so only a page that returned rows carries them. An empty
			// final page reports zero, which is the one number a caller must not
			// read as "there are none".
			test.EqOp(t, uint64(5), page.FilteredCount)

			filter.SetCursor(&page.Cursor)
		}

		test.EqOp(t, 5, walked)
	})
}

func TestStore_GetMany(T *testing.T) {
	T.Parallel()

	T.Run("reads the named rows in one statement", func(t *testing.T) {
		t.Parallel()

		store, _ := newCommentStore(t)
		ctx := t.Context()

		var ids []string

		for i := range 3 {
			created, err := store.Create(ctx, tenancy.Global(), alice(), comment(string(rune('a'+i)), "recipe_many", "user_alice"))
			must.NoError(t, err)

			ids = append(ids, created.ID)
		}

		rows, err := store.GetMany(ctx, tenancy.Global(), bob(), ids...)
		must.NoError(t, err)
		must.SliceLen(t, 3, rows)

		found := map[string]string{}
		for _, row := range rows {
			found[row.ID] = row.Content
		}

		test.MapLen(t, 3, found)
	})

	T.Run("an id that is gone is one fewer row rather than an error", func(t *testing.T) {
		t.Parallel()

		store, _ := newCommentStore(t)
		ctx := t.Context()

		kept, err := store.Create(ctx, tenancy.Global(), alice(), comment("kept", "recipe_partial", "user_alice"))
		must.NoError(t, err)

		archived, err := store.Create(ctx, tenancy.Global(), alice(), comment("archived", "recipe_partial", "user_alice"))
		must.NoError(t, err)
		must.NoError(t, store.Archive(ctx, tenancy.Global(), alice(), archived.ID))

		rows, err := store.GetMany(ctx, tenancy.Global(), alice(), kept.ID, archived.ID, "comment_that_never_existed")
		must.NoError(t, err)
		must.SliceLen(t, 1, rows)
		test.EqOp(t, kept.ID, rows[0].ID)
	})

	T.Run("no ids is no query", func(t *testing.T) {
		t.Parallel()

		store, _ := newCommentStore(t)

		rows, err := store.GetMany(t.Context(), tenancy.Global(), alice())
		must.NoError(t, err)
		test.SliceEmpty(t, rows)
	})

	T.Run("an empty id is refused", func(t *testing.T) {
		t.Parallel()

		store, _ := newCommentStore(t)

		_, err := store.GetMany(t.Context(), tenancy.Global(), alice(), "comment_1", "")
		test.ErrorIs(t, err, platformerrors.ErrInvalidIDProvided)
	})
}

func TestStore_Dimensions(T *testing.T) {
	T.Parallel()

	T.Run("a scope naming a tenant is refused by an unscoped resource", func(t *testing.T) {
		t.Parallel()

		store, _ := newCommentStore(t)

		_, err := store.Get(t.Context(), tenancy.Of("account_1"), alice(), "whatever")
		test.ErrorIs(t, err, resources.ErrScopeNotSupported)
	})

	T.Run("an unset scope is refused", func(t *testing.T) {
		t.Parallel()

		store, _ := newCommentStore(t)

		_, err := store.Get(t.Context(), tenancy.Scope{}, alice(), "whatever")
		must.Error(t, err)
	})

	T.Run("an unset actor is refused", func(t *testing.T) {
		t.Parallel()

		store, _ := newCommentStore(t)

		_, err := store.Get(t.Context(), tenancy.Global(), resources.Actor{}, "whatever")
		test.ErrorIs(t, err, resources.ErrNoActor)
	})

	T.Run("an empty id is refused", func(t *testing.T) {
		t.Parallel()

		store, _ := newCommentStore(t)

		_, err := store.Get(t.Context(), tenancy.Global(), alice(), "")
		test.ErrorIs(t, err, platformerrors.ErrInvalidIDProvided)
	})
}

func TestStore_Hooks(T *testing.T) {
	T.Parallel()

	T.Run("a hook sees the stored row", func(t *testing.T) {
		t.Parallel()

		store, seen := newCommentStore(t)

		mark := seen.mark()

		created, err := store.Create(t.Context(), tenancy.Global(), alice(), comment("hooked", "recipe_hook", "user_alice"))
		must.NoError(t, err)

		recorded := seen.since(mark)
		must.SliceLen(t, 1, recorded)

		change := recorded[0]
		test.EqOp(t, resources.OpCreated, change.Op)
		test.EqOp(t, "comment", change.Resource)
		test.EqOp(t, "comments", change.Table)
		test.EqOp(t, created.ID, change.ID)
		test.EqOp(t, "user_alice", change.Owner)
		test.EqOp(t, "user_alice", change.Actor.ID())
		must.NotNil(t, change.Row)
		test.EqOp(t, "hooked", change.Row.Content)
	})

	T.Run("a failing hook rolls the write back", func(t *testing.T) {
		t.Parallel()

		client := newSQLiteClient(t)
		ctx := t.Context()

		resource, err := resources.Define(dialect.SQLite, commentsDefinition())
		must.NoError(t, err)

		boom := platformerrors.New("boom")

		failing, err := resources.NewStore(resource, client,
			resources.WithHook(func(_ context.Context, _ database.SQLQueryExecutor, _ resources.Change[Comment]) error {
				return boom
			}),
		)
		must.NoError(t, err)

		created, err := failing.Create(ctx, tenancy.Global(), alice(), comment("rolled back", "recipe_rollback", "user_alice"))
		test.ErrorIs(t, err, boom)
		test.Nil(t, created)

		reader, err := resources.NewStore(resource, client)
		must.NoError(t, err)

		page, err := reader.List(ctx, tenancy.Global(), alice(), nil,
			resources.By("target_type", "recipes"),
			resources.By("referenced_id", "recipe_rollback"),
		)
		must.NoError(t, err)
		test.SliceLen(t, 0, page.Data)
	})

	T.Run("a hook for another row type is refused at construction", func(t *testing.T) {
		t.Parallel()

		client := newSQLiteClient(t)

		resource, err := resources.Define(dialect.SQLite, commentsDefinition())
		must.NoError(t, err)

		_, err = resources.NewStore(resource, client,
			resources.WithHook(func(_ context.Context, _ database.SQLQueryExecutor, _ resources.Change[string]) error {
				return nil
			}),
		)
		test.ErrorIs(t, err, resources.ErrHookTypeMismatch)
	})
}

func TestNewStore(T *testing.T) {
	T.Parallel()

	T.Run("refuses a resource rendered for another dialect", func(t *testing.T) {
		t.Parallel()

		client := newSQLiteClient(t)

		resource, err := resources.Define(dialect.Postgres, commentsDefinition())
		must.NoError(t, err)

		_, err = resources.NewStore(resource, client)
		test.ErrorIs(t, err, dialect.ErrUnsupported)
	})

	T.Run("refuses nil dependencies", func(t *testing.T) {
		t.Parallel()

		resource, err := resources.Define(dialect.SQLite, commentsDefinition())
		must.NoError(t, err)

		_, err = resources.NewStore(resource, nil)
		test.ErrorIs(t, err, platformerrors.ErrNilInputParameter)

		_, err = resources.NewStore[Comment](nil, newSQLiteClient(t))
		test.ErrorIs(t, err, platformerrors.ErrNilInputParameter)
	})
}

// alice and bob are the two authors the suite writes as.
func alice() resources.Actor { return resources.ActingAs("user_alice") }
func bob() resources.Actor   { return resources.ActingAs("user_bob") }

// comment builds a comment on a recipe.
func comment(content, reference, author string) *Comment {
	return &Comment{
		Content:       content,
		TargetType:    "recipes",
		ReferencedID:  reference,
		BelongsToUser: author,
	}
}

// issueComment builds a comment on an issue report, for the tests that need two
// target types over one reference.
func issueComment(content, reference, author string) *Comment {
	row := comment(content, reference, author)
	row.TargetType = "issue_reports"

	return row
}
