package resources_test

import (
	"context"
	"testing"

	"github.com/primandproper/platform-go/v12/database/dialect"
	"github.com/primandproper/platform-go/v12/database/postgres"
	"github.com/primandproper/platform-go/v12/resources"
	"github.com/primandproper/platform-go/v12/tenancy"
	"github.com/primandproper/platform-go/v12/testutils/containers/pgtest"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// commentsSchema is dinnerdonebetter's 00012_comments.sql, verbatim but for the
// users foreign key, whose table this suite does not create.
//
// It is copied rather than approximated because the point of this suite is
// whether a declaration can serve a table an application already has — including
// the parts a generic kit would rather not have met, which here is that
// target_type is a Postgres enum rather than text.
const commentsSchema = `
CREATE TYPE comment_target_type AS ENUM ('issue_reports', 'recipes');

CREATE TABLE IF NOT EXISTS comments (
    id TEXT NOT NULL PRIMARY KEY,
    content TEXT NOT NULL DEFAULT '',
    target_type comment_target_type NOT NULL,
    referenced_id TEXT NOT NULL,
    parent_comment_id TEXT REFERENCES comments("id") ON DELETE CASCADE,
    belongs_to_user TEXT NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    last_updated_at TIMESTAMP WITH TIME ZONE,
    archived_at TIMESTAMP WITH TIME ZONE
);

CREATE INDEX idx_comments_reference ON comments (target_type, referenced_id) WHERE archived_at IS NULL;
CREATE INDEX idx_comments_user ON comments (belongs_to_user) WHERE archived_at IS NULL;
CREATE INDEX idx_comments_parent ON comments (parent_comment_id) WHERE archived_at IS NULL;
`

// TestComments_RealServer runs the declaration against the table it was written
// for, on the server that table lives on.
//
// What only Postgres can answer is here rather than in the SQLite suite: an enum
// column bound as text, a timestamptz round trip, the id set bound as one array
// argument, and the deferred cascade a real foreign key performs.
func TestComments_RealServer(T *testing.T) {
	T.Parallel()

	pgtest.Run(T, func(_ context.Context, pg *pgtest.Instance) {
		client, err := postgres.NewDatabaseClient(T.Context(),
			&testClientConfig{connectionString: pg.ConnectionString})
		must.NoError(T, err)
		T.Cleanup(func() { _ = client.Close() })

		mustExec(T, client, commentsSchema)

		resource, err := resources.Define(dialect.Postgres, commentsDefinition())
		must.NoError(T, err)

		// One table, one store per subtest. The subtests run in parallel over
		// the same comments table and keep out of each other's way by writing
		// against their own reference, which is how the application's own
		// concurrent traffic keeps out of its own way.
		newStore := func(t *testing.T) (*resources.Store[Comment], *changeLog) {
			t.Helper()

			seen := &changeLog{}

			store, storeErr := resources.NewStore(resource, client, resources.WithHook(seen.record))
			must.NoError(t, storeErr)

			return store, seen
		}

		ctx := T.Context()
		global := tenancy.Global()

		T.Run("an enum column round-trips as the string it is declared as", func(t *testing.T) {
			t.Parallel()

			store, _ := newStore(t)

			created, createErr := store.Create(ctx, global, alice(), comment("the roux needs longer", "recipe_1", "user_alice"))
			must.NoError(t, createErr)

			test.EqOp(t, "recipes", created.TargetType)
			test.False(t, created.CreatedAt.IsZero())

			// The other enum value, so the column is exercised rather than the
			// one literal.
			other, createErr := store.Create(ctx, global, alice(), issueComment("on the report", "report_1", "user_alice"))
			must.NoError(t, createErr)
			test.EqOp(t, "issue_reports", other.TargetType)
		})

		T.Run("a nullable column round-trips as a pointer", func(t *testing.T) {
			t.Parallel()

			store, _ := newStore(t)

			parent, createErr := store.Create(ctx, global, alice(), comment("parent", "recipe_nullable", "user_alice"))
			must.NoError(t, createErr)

			reply := comment("reply", "recipe_nullable", "user_bob")
			reply.ParentCommentID = &parent.ID

			stored, createErr := store.Create(ctx, global, bob(), reply)
			must.NoError(t, createErr)
			must.NotNil(t, stored.ParentCommentID)
			test.EqOp(t, parent.ID, *stored.ParentCommentID)
			test.Nil(t, stored.LastUpdatedAt)
		})

		T.Run("the set read binds one array argument", func(t *testing.T) {
			t.Parallel()

			store, _ := newStore(t)

			var ids []string

			for i := range 3 {
				created, createErr := store.Create(ctx, global, alice(),
					comment(string(rune('a'+i)), "recipe_set", "user_alice"))
				must.NoError(t, createErr)

				ids = append(ids, created.ID)
			}

			// On Postgres the whole set is one placeholder, which is the binding
			// the pilot never executed — see the issue this package came from.
			rows, getErr := store.GetMany(ctx, global, bob(), ids...)
			must.NoError(t, getErr)
			must.SliceLen(t, 3, rows)

			// And a set of one, which is where an expansion bug would hide on the
			// dialects that expand.
			rows, getErr = store.GetMany(ctx, global, bob(), ids[0])
			must.NoError(t, getErr)
			must.SliceLen(t, 1, rows)
			test.EqOp(t, ids[0], rows[0].ID)
		})

		T.Run("only the owner may update", func(t *testing.T) {
			t.Parallel()

			store, _ := newStore(t)

			created, createErr := store.Create(ctx, global, alice(), comment("original", "recipe_owner", "user_alice"))
			must.NoError(t, createErr)

			created.Content = "edited by bob"
			_, updateErr := store.Update(ctx, global, bob(), created)
			test.ErrorIs(t, updateErr, resources.ErrNoRowsAffected)

			created.Content = "edited by alice"
			updated, updateErr := store.Update(ctx, global, alice(), created)
			must.NoError(t, updateErr)
			test.EqOp(t, "edited by alice", updated.Content)
			must.NotNil(t, updated.LastUpdatedAt)
		})

		T.Run("the reference lookup returns every author's comments", func(t *testing.T) {
			t.Parallel()

			store, _ := newStore(t)

			for _, c := range []*Comment{
				comment("a", "recipe_list", "user_alice"),
				comment("b", "recipe_list", "user_bob"),
				issueComment("c", "recipe_list", "user_bob"),
			} {
				_, createErr := store.Create(ctx, global, alice(), c)
				must.NoError(t, createErr)
			}

			page, listErr := store.List(ctx, global, alice(), nil,
				resources.By("target_type", "recipes"),
				resources.By("referenced_id", "recipe_list"),
			)
			must.NoError(t, listErr)
			test.SliceLen(t, 2, page.Data)
			test.EqOp(t, uint64(2), page.FilteredCount)
		})

		T.Run("a cascade archives every author's rows and reports each one", func(t *testing.T) {
			t.Parallel()

			store, seen := newStore(t)

			for _, c := range []*Comment{
				comment("x", "recipe_cascade", "user_alice"),
				comment("y", "recipe_cascade", "user_bob"),
			} {
				_, createErr := store.Create(ctx, global, alice(), c)
				must.NoError(t, createErr)
			}

			mark := seen.mark()

			matches := []resources.Match{
				resources.By("target_type", "recipes"),
				resources.By("referenced_id", "recipe_cascade"),
			}

			must.NoError(t, store.ArchiveMatching(ctx, global, resources.System(), matches...))

			page, listErr := store.List(ctx, global, alice(), nil, matches...)
			must.NoError(t, listErr)
			test.SliceLen(t, 0, page.Data)

			test.SliceLen(t, 2, seen.since(mark))
		})

		T.Run("a foreign key's cascade is the database's business", func(t *testing.T) {
			t.Parallel()

			store, _ := newStore(t)

			parent, createErr := store.Create(ctx, global, alice(), comment("parent", "recipe_fk", "user_alice"))
			must.NoError(t, createErr)

			reply := comment("reply", "recipe_fk", "user_alice")
			reply.ParentCommentID = &parent.ID

			stored, createErr := store.Create(ctx, global, alice(), reply)
			must.NoError(t, createErr)

			// Archiving the parent is a soft delete, so the real foreign key is
			// untouched and the reply is still readable — which is the answer a
			// soft-deleting store should give, and worth pinning against a
			// server that would enforce the alternative.
			must.NoError(t, store.Archive(ctx, global, alice(), parent.ID))

			found, getErr := store.Get(ctx, global, alice(), stored.ID)
			must.NoError(t, getErr)
			must.NotNil(t, found.ParentCommentID)
			test.EqOp(t, parent.ID, *found.ParentCommentID)
		})
	})
}
