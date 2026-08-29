package comments

import (
	"errors"
	"testing"

	"github.com/primandproper/platform-go/v13/database"
	"github.com/primandproper/platform-go/v13/database/dialect"
	"github.com/primandproper/platform-go/v13/filtering"
	"github.com/primandproper/platform-go/v13/pointer"
	"github.com/primandproper/platform-go/v13/tenancy"

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
// archived predicates and the :execrows count every write reads as its answer
// are spelled three ways, and a suite that ran only against SQLite would prove
// the one spelling SQLite accepts.
func runStoreSuite(t *testing.T, env *storeEnv) {
	t.Helper()

	t.Run("writes", func(t *testing.T) {
		t.Parallel()

		runWriteSuite(t, env)
	})

	t.Run("targets", func(t *testing.T) {
		t.Parallel()

		runTargetSuite(t, env)
	})

	t.Run("threads", func(t *testing.T) {
		t.Parallel()

		runThreadSuite(t, env)
	})

	t.Run("reads", func(t *testing.T) {
		t.Parallel()

		runReadSuite(t, env)
	})

	t.Run("sweeps", func(t *testing.T) {
		t.Parallel()

		runSweepSuite(t, env)
	})
}

func runWriteSuite(t *testing.T, env *storeEnv) {
	t.Helper()

	t.Run("a written comment comes back with what the database assigned", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		c := newComment(testAuthor, "this was delicious")
		must.NoError(t, store.CreateComment(t.Context(), c))

		// The id is minted here and the creation time is the database's, read
		// back rather than left as a zero time a caller would serialize as a
		// date in the year one.
		test.NotEqOp(t, "", c.ID)
		test.False(t, c.CreatedAt.IsZero())
		test.Nil(t, c.LastUpdatedAt)
		test.True(t, c.Root())

		read, err := store.GetComment(t.Context(), testScope, c.ID)
		must.NoError(t, err)
		test.EqOp(t, c.ID, read.ID)
		test.EqOp(t, testAuthor, read.Author)
		test.EqOp(t, "this was delicious", read.Body)
		test.EqOp(t, testTarget, read.Target)
		test.EqOp(t, RootParentID, read.ParentID)
	})

	t.Run("a caller-supplied id is kept", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		c := newComment(testAuthor, "words")
		c.ID = "comment_of_my_own"
		must.NoError(t, store.CreateComment(t.Context(), c))

		read, err := store.GetComment(t.Context(), testScope, "comment_of_my_own")
		must.NoError(t, err)
		test.EqOp(t, "comment_of_my_own", read.ID)
	})

	t.Run("a comment written by nobody or saying nothing is refused", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		anonymous := newComment("", "words")
		must.ErrorIs(t, store.CreateComment(t.Context(), anonymous), ErrEmptyAuthor)

		// Whitespace is nothing said. A row holding it is a comment a client
		// renders as an empty bubble.
		silent := newComment(testAuthor, "   \n\t ")
		must.ErrorIs(t, store.CreateComment(t.Context(), silent), ErrEmptyBody)
	})

	t.Run("a nil comment and an unset scope are refused", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		must.ErrorIs(t, store.CreateComment(t.Context(), nil), ErrNilComment)

		unscoped := newComment(testAuthor, "words")
		unscoped.Scope = tenancy.Scope{}
		test.Error(t, store.CreateComment(t.Context(), unscoped))
	})

	t.Run("an edit revises the body and stamps the row", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		c := written(t, store, newComment(testAuthor, "frist"))

		c.Body = "first"
		must.NoError(t, store.UpdateComment(t.Context(), c))

		read, err := store.GetComment(t.Context(), testScope, c.ID)
		must.NoError(t, err)
		test.EqOp(t, "first", read.Body)

		// The stamp a client renders "edited" from. It is nil until somebody
		// revises the comment, which is what makes it readable as a fact about
		// the words rather than about the row.
		must.NotNil(t, read.LastUpdatedAt)
	})

	t.Run("an edit cannot move a comment to another target, parent or author", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		root := written(t, store, newComment(testAuthor, "root"))
		child := written(t, store, reply(root.ID, otherAuthor, "child"))

		// Everything but the body changed, which is everything the statement
		// does not assign: the write succeeds and none of it lands.
		child.Body = "child, edited"
		child.Target = Target{Type: mealType, ID: "meal_9"}
		child.ParentID = RootParentID
		child.Author = "somebody_else"
		must.NoError(t, store.UpdateComment(t.Context(), child))

		read, err := store.GetComment(t.Context(), testScope, child.ID)
		must.NoError(t, err)
		test.EqOp(t, "child, edited", read.Body)
		test.EqOp(t, testTarget, read.Target)
		test.EqOp(t, root.ID, read.ParentID)
		test.EqOp(t, otherAuthor, read.Author)
	})

	t.Run("an edit that empties the body is refused", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		c := written(t, store, newComment(testAuthor, "words"))

		c.Body = " "
		must.ErrorIs(t, store.UpdateComment(t.Context(), c), ErrEmptyBody)

		read, err := store.GetComment(t.Context(), testScope, c.ID)
		must.NoError(t, err)
		test.EqOp(t, "words", read.Body)
	})

	t.Run("a write cannot reach another scope's comment", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		c := written(t, store, newComment(testAuthor, "mine"))

		theirs := *c
		theirs.Scope = otherScope
		theirs.Body = "not yours to edit"

		must.ErrorIs(t, store.UpdateComment(t.Context(), &theirs), ErrCommentNotFound)
		must.ErrorIs(t, store.ArchiveComment(t.Context(), otherScope, c.ID), ErrCommentNotFound)

		read, err := store.GetComment(t.Context(), testScope, c.ID)
		must.NoError(t, err)
		test.EqOp(t, "mine", read.Body)
	})

	t.Run("an archived comment leaves the discussion and cannot be archived twice", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		c := written(t, store, newComment(testAuthor, "off topic"))

		must.NoError(t, store.ArchiveComment(t.Context(), testScope, c.ID))

		_, err := store.GetComment(t.Context(), testScope, c.ID)
		must.ErrorIs(t, err, ErrCommentNotFound)

		// The statement excludes archived rows, so a second archive addresses
		// nothing — which is the honest answer rather than a quiet success.
		must.ErrorIs(t, store.ArchiveComment(t.Context(), testScope, c.ID), ErrCommentNotFound)

		page, err := store.ListRootComments(t.Context(), testScope, testTarget, nil)
		must.NoError(t, err)
		test.SliceEmpty(t, page.Data)
	})
}

func runTargetSuite(t *testing.T, env *storeEnv) {
	t.Helper()

	t.Run("a target type nobody registered is refused", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		c := newComment(testAuthor, "about a thing that does not exist here")
		c.Target = Target{Type: unknownType, ID: "x"}

		must.ErrorIs(t, store.CreateComment(t.Context(), c), ErrUnknownTargetType)
	})

	t.Run("a store with no catalog accepts nothing", func(t *testing.T) {
		t.Parallel()

		// The reading webhooks takes of an absent event catalog. A store built
		// without one is a wiring mistake, and failing on the first write beats
		// storing rows under types nothing will ever list.
		store := env.newStore(t, WithTargets(Targets{}))

		must.ErrorIs(t, store.CreateComment(t.Context(), newComment(testAuthor, "words")),
			ErrUnknownTargetType)
	})

	t.Run("a malformed target is refused before the catalog is consulted", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		typeless := newComment(testAuthor, "words")
		typeless.Target = Target{ID: "recipe_1"}
		must.ErrorIs(t, store.CreateComment(t.Context(), typeless), ErrEmptyTargetType)

		// The empty id is not a wildcard: a comment holding it would be about
		// every recipe and no recipe at once.
		idless := newComment(testAuthor, "words")
		idless.Target = Target{Type: recipeType}
		must.ErrorIs(t, store.CreateComment(t.Context(), idless), ErrEmptyTargetID)
	})

	t.Run("a registered existence check is consulted, in the comment's scope", func(t *testing.T) {
		t.Parallel()

		check := newRecordingCheck(true, nil)
		store := env.newStore(t, WithTargets(Targets{
			recipeType: {Description: "a recipe", Exists: check.exists},
		}))

		must.NoError(t, store.CreateComment(t.Context(), newComment(testAuthor, "words")))

		test.Eq(t, []string{testTarget.ID}, check.asked)
		test.Eq(t, []tenancy.Scope{testScope}, check.scopes)
	})

	t.Run("a target the check cannot find is refused and nothing is written", func(t *testing.T) {
		t.Parallel()

		check := newRecordingCheck(false, nil)
		store := env.newStore(t, WithTargets(Targets{
			recipeType: {Description: "a recipe", Exists: check.exists},
		}))

		c := newComment(testAuthor, "about a deleted recipe")
		must.ErrorIs(t, store.CreateComment(t.Context(), c), ErrTargetNotFound)

		page, err := store.ListCommentsByTargetType(t.Context(), testScope, recipeType, nil)
		must.NoError(t, err)
		test.SliceEmpty(t, page.Data)
	})

	t.Run("a check that fails is not a target that is absent", func(t *testing.T) {
		t.Parallel()

		// The two lead to opposite actions and only one of them is recoverable by
		// trying again, so a hook that could not reach its table fails the write
		// rather than deciding the target is gone.
		check := newRecordingCheck(false, errCheckUnavailable)
		store := env.newStore(t, WithTargets(Targets{
			recipeType: {Description: "a recipe", Exists: check.exists},
		}))

		err := store.CreateComment(t.Context(), newComment(testAuthor, "words"))
		must.ErrorIs(t, err, errCheckUnavailable)
		test.False(t, errors.Is(err, ErrTargetNotFound))
	})

	t.Run("the catalog is copied, so a later mutation changes nothing", func(t *testing.T) {
		t.Parallel()

		catalog := Targets{recipeType: {Description: "a recipe"}}
		store := env.newStore(t, WithTargets(catalog))

		catalog[unknownType] = TargetDefinition{Description: "smuggled in afterwards"}

		c := newComment(testAuthor, "words")
		c.Target = Target{Type: unknownType, ID: "x"}

		must.ErrorIs(t, store.CreateComment(t.Context(), c), ErrUnknownTargetType)
		test.Eq(t, []TargetType{recipeType}, store.TargetTypes())
	})

	t.Run("a read is not gated on the catalog", func(t *testing.T) {
		t.Parallel()

		// The withdrawal case, which is the whole reason reads are ungated: the
		// rows written under a type that has since left the catalog are exactly
		// the ones an operator needs to reach.
		store, prefix := env.newStoreWithPrefix(t)
		written(t, store, newComment(testAuthor, "written while the type was live"))

		withdrawn, err := NewSQLStore(env.client,
			WithTablePrefix(prefix), WithTargets(Targets{mealType: {Description: "a meal"}}))
		must.NoError(t, err)

		page, err := withdrawn.ListCommentsByTargetType(t.Context(), testScope, recipeType, nil)
		must.NoError(t, err)
		must.SliceLen(t, 1, page.Data)

		// And the write is still refused, which is the asymmetry stated as a
		// pair rather than as one half.
		must.ErrorIs(t, withdrawn.CreateComment(t.Context(), newComment(testAuthor, "too late")),
			ErrUnknownTargetType)
	})
}

func runThreadSuite(t *testing.T, env *storeEnv) {
	t.Helper()

	t.Run("a reply adopts its parent's target", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		root := written(t, store, newComment(testAuthor, "root"))

		child := reply(root.ID, otherAuthor, "child")
		must.NoError(t, store.CreateComment(t.Context(), child))

		test.EqOp(t, testTarget, child.Target)

		read, err := store.GetComment(t.Context(), testScope, child.ID)
		must.NoError(t, err)
		test.EqOp(t, testTarget, read.Target)
		test.EqOp(t, root.ID, read.ParentID)
		test.False(t, read.Root())
	})

	t.Run("a reply naming its parent's target is accepted", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		root := written(t, store, newComment(testAuthor, "root"))

		child := reply(root.ID, otherAuthor, "child")
		child.Target = testTarget

		must.NoError(t, store.CreateComment(t.Context(), child))
	})

	t.Run("a reply naming a different target is refused", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		root := written(t, store, newComment(testAuthor, "root"))

		child := reply(root.ID, otherAuthor, "child")
		child.Target = Target{Type: mealType, ID: "meal_9"}

		must.ErrorIs(t, store.CreateComment(t.Context(), child), ErrTargetMismatch)
	})

	t.Run("a reply to a reply is refused", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		root := written(t, store, newComment(testAuthor, "root"))
		child := written(t, store, reply(root.ID, otherAuthor, "child"))

		grandchild := reply(child.ID, testAuthor, "grandchild")
		must.ErrorIs(t, store.CreateComment(t.Context(), grandchild), ErrNestedReply)
	})

	t.Run("a reply to a comment that is not in the scope is refused", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		orphan := reply("no_such_comment", testAuthor, "into the void")
		must.ErrorIs(t, store.CreateComment(t.Context(), orphan), ErrParentNotFound)

		// Another scope's comment reads as absent from here, which is the same
		// answer and for the same reason a get gives it.
		mine := written(t, store, newComment(testAuthor, "root"))

		crossScope := reply(mine.ID, testAuthor, "from next door")
		crossScope.Scope = otherScope
		must.ErrorIs(t, store.CreateComment(t.Context(), crossScope), ErrParentNotFound)
	})

	t.Run("a reply to an archived comment is refused", func(t *testing.T) {
		t.Parallel()

		// A moderator removed the root; replying to it now would put a comment
		// under something no discussion renders.
		store := env.newStore(t)
		root := written(t, store, newComment(testAuthor, "root"))
		must.NoError(t, store.ArchiveComment(t.Context(), testScope, root.ID))

		must.ErrorIs(t, store.CreateComment(t.Context(), reply(root.ID, otherAuthor, "late")),
			ErrParentNotFound)
	})

	t.Run("archiving a root leaves its replies where they are", func(t *testing.T) {
		t.Parallel()

		// The ruling in the package doc: a reply outlives the comment it replies
		// to, and "in reply to a removed comment" is what a discussion renders.
		store := env.newStore(t)
		root := written(t, store, newComment(testAuthor, "root"))
		child := written(t, store, reply(root.ID, otherAuthor, "child"))

		must.NoError(t, store.ArchiveComment(t.Context(), testScope, root.ID))

		replies, err := store.ListReplies(t.Context(), testScope, testTarget, root.ID, nil)
		must.NoError(t, err)
		must.SliceLen(t, 1, replies.Data)
		test.EqOp(t, child.ID, replies.Data[0].ID)
	})

	t.Run("the roots and the replies are different halves of the discussion", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		first := written(t, store, newComment(testAuthor, "first root"))
		second := written(t, store, newComment(otherAuthor, "second root"))
		child := written(t, store, reply(first.ID, otherAuthor, "a reply"))

		roots, err := store.ListRootComments(t.Context(), testScope, testTarget, nil)
		must.NoError(t, err)
		must.SliceLen(t, 2, roots.Data)
		test.Eq(t, []string{first.ID, second.ID}, ids(roots.Data))

		replies, err := store.ListReplies(t.Context(), testScope, testTarget, first.ID, nil)
		must.NoError(t, err)
		must.SliceLen(t, 1, replies.Data)
		test.EqOp(t, child.ID, replies.Data[0].ID)

		// The other root has none, which is a page rather than an error.
		none, err := store.ListReplies(t.Context(), testScope, testTarget, second.ID, nil)
		must.NoError(t, err)
		test.SliceEmpty(t, none.Data)
	})

	t.Run("a reply read that names no parent is refused rather than answered with the roots", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		written(t, store, newComment(testAuthor, "root"))

		_, err := store.ListReplies(t.Context(), testScope, testTarget, RootParentID, nil)
		must.ErrorIs(t, err, ErrEmptyParent)
	})
}

func runReadSuite(t *testing.T, env *storeEnv) {
	t.Helper()

	t.Run("a discussion is scoped to its target and its tenant", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		mine := written(t, store, newComment(testAuthor, "on recipe_1"))

		elsewhere := newComment(testAuthor, "on recipe_2")
		elsewhere.Target = Target{Type: recipeType, ID: "recipe_2"}
		written(t, store, elsewhere)

		nextDoor := newComment(testAuthor, "in another tenant")
		nextDoor.Scope = otherScope
		written(t, store, nextDoor)

		page, err := store.ListRootComments(t.Context(), testScope, testTarget, nil)
		must.NoError(t, err)
		must.SliceLen(t, 1, page.Data)
		test.EqOp(t, mine.ID, page.Data[0].ID)
	})

	t.Run("a target-type list spans every target of the type, replies included", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		root := written(t, store, newComment(testAuthor, "on recipe_1"))
		child := written(t, store, reply(root.ID, otherAuthor, "a reply"))

		second := newComment(testAuthor, "on recipe_2")
		second.Target = Target{Type: recipeType, ID: "recipe_2"}
		written(t, store, second)

		meal := newComment(testAuthor, "on a meal")
		meal.Target = Target{Type: mealType, ID: "meal_1"}
		written(t, store, meal)

		page, err := store.ListCommentsByTargetType(t.Context(), testScope, recipeType, nil)
		must.NoError(t, err)
		test.Eq(t, []string{root.ID, child.ID, second.ID}, ids(page.Data))
	})

	t.Run("an author's list is theirs alone", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		mine := written(t, store, newComment(testAuthor, "mine"))
		written(t, store, newComment(otherAuthor, "theirs"))

		page, err := store.ListCommentsByAuthor(t.Context(), testScope, testAuthor, nil)
		must.NoError(t, err)
		must.SliceLen(t, 1, page.Data)
		test.EqOp(t, mine.ID, page.Data[0].ID)
	})

	t.Run("an archived comment is out of every list until it is asked for", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		c := written(t, store, newComment(testAuthor, "off topic"))
		must.NoError(t, store.ArchiveComment(t.Context(), testScope, c.ID))

		hidden, err := store.ListCommentsByAuthor(t.Context(), testScope, testAuthor, nil)
		must.NoError(t, err)
		test.SliceEmpty(t, hidden.Data)

		shown, err := store.ListCommentsByAuthor(t.Context(), testScope, testAuthor,
			&filtering.QueryFilter{IncludeArchived: pointer.To(true)})
		must.NoError(t, err)
		must.SliceLen(t, 1, shown.Data)
		must.NotNil(t, shown.Data[0].ArchivedAt)
	})

	t.Run("a descending page walks the same rows the other way", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		first := written(t, store, newComment(testAuthor, "first"))
		second := written(t, store, newComment(testAuthor, "second"))

		descending := &filtering.QueryFilter{SortBy: filtering.SortDescending}

		page, err := store.ListRootComments(t.Context(), testScope, testTarget, descending)
		must.NoError(t, err)
		test.Eq(t, []string{second.ID, first.ID}, ids(page.Data))
	})

	t.Run("a page carries the count of everything it is a page of", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		for _, body := range []string{"one", "two", "three"} {
			written(t, store, newComment(testAuthor, body))
		}

		page, err := store.ListRootComments(t.Context(), testScope, testTarget,
			&filtering.QueryFilter{MaxResponseSize: pointer.To(uint16(2))})
		must.NoError(t, err)

		must.SliceLen(t, 2, page.Data)
		test.EqOp(t, uint64(3), page.FilteredCount)
	})

	t.Run("a read of an absent or another tenant's comment is the same answer", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		c := written(t, store, newComment(testAuthor, "mine"))

		_, err := store.GetComment(t.Context(), testScope, "no_such_comment")
		must.ErrorIs(t, err, ErrCommentNotFound)

		_, err = store.GetComment(t.Context(), otherScope, c.ID)
		must.ErrorIs(t, err, ErrCommentNotFound)
	})

	t.Run("a read that names nothing to read is refused", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		_, err := store.ListCommentsByAuthor(t.Context(), testScope, "", nil)
		must.ErrorIs(t, err, ErrEmptyAuthor)

		_, err = store.ListCommentsByTargetType(t.Context(), testScope, "", nil)
		must.ErrorIs(t, err, ErrEmptyTargetType)

		_, err = store.ListRootComments(t.Context(), testScope, Target{Type: recipeType}, nil)
		must.ErrorIs(t, err, ErrEmptyTargetID)
	})
}

func runSweepSuite(t *testing.T, env *storeEnv) {
	t.Helper()

	t.Run("a sweep destroys every comment about one thing, archived and replies alike", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		root := written(t, store, newComment(testAuthor, "root"))
		written(t, store, reply(root.ID, otherAuthor, "child"))

		gone := written(t, store, newComment(testAuthor, "already archived"))
		must.NoError(t, store.ArchiveComment(t.Context(), testScope, gone.ID))

		elsewhere := newComment(testAuthor, "about another recipe")
		elsewhere.Target = Target{Type: recipeType, ID: "recipe_2"}
		survivor := written(t, store, elsewhere)

		deleted := sweep(t, store, testScope, testTarget)
		test.EqOp(t, int64(3), deleted)

		page, err := store.ListCommentsByTargetType(t.Context(), testScope, recipeType, nil)
		must.NoError(t, err)
		must.SliceLen(t, 1, page.Data)
		test.EqOp(t, survivor.ID, page.Data[0].ID)
	})

	t.Run("a sweep of a type the catalog no longer holds still runs", func(t *testing.T) {
		t.Parallel()

		// The whole point of the sweep: a target type on its way out is exactly
		// the one whose rows have to be reachable.
		store, prefix := env.newStoreWithPrefix(t)
		written(t, store, newComment(testAuthor, "written while the type was live"))

		withdrawn, err := NewSQLStore(env.client,
			WithTablePrefix(prefix), WithTargets(Targets{mealType: {Description: "a meal"}}))
		must.NoError(t, err)

		test.EqOp(t, int64(1), sweep(t, withdrawn, testScope, testTarget))
	})

	t.Run("a sweep cannot reach another scope", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		c := written(t, store, newComment(testAuthor, "mine"))

		test.EqOp(t, int64(0), sweep(t, store, otherScope, testTarget))

		_, err := store.GetComment(t.Context(), testScope, c.ID)
		must.NoError(t, err)
	})

	t.Run("an erasure destroys one author's comments and nobody else's", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		written(t, store, newComment(testAuthor, "mine"))

		// Archived and still theirs: an erasure has to reach what a soft delete
		// hid, or a subject's words survive their own request.
		archived := written(t, store, newComment(testAuthor, "archived but still mine"))
		must.NoError(t, store.ArchiveComment(t.Context(), testScope, archived.ID))

		theirs := written(t, store, newComment(otherAuthor, "theirs"))

		var deleted int64

		must.NoError(t, store.client.WithTransaction(t.Context(), func(tx database.Tx) error {
			var err error
			deleted, err = store.DeleteCommentsByAuthor(t.Context(), tx, testScope, testAuthor)

			return err
		}))

		test.EqOp(t, int64(2), deleted)

		mine, err := store.ListCommentsByAuthor(t.Context(), testScope, testAuthor, nil)
		must.NoError(t, err)
		test.SliceEmpty(t, mine.Data)

		survivor, err := store.GetComment(t.Context(), testScope, theirs.ID)
		must.NoError(t, err)
		test.EqOp(t, otherAuthor, survivor.Author)
	})

	t.Run("an erasure leaves the replies to what it erased", func(t *testing.T) {
		t.Parallel()

		// Cascading would mean erasing other people's words to satisfy one
		// person's request. The reply stays and its parent is gone, which is the
		// same state a moderator's archive produces.
		store := env.newStore(t)
		root := written(t, store, newComment(testAuthor, "root"))
		child := written(t, store, reply(root.ID, otherAuthor, "child"))

		must.NoError(t, store.client.WithTransaction(t.Context(), func(tx database.Tx) error {
			_, err := store.DeleteCommentsByAuthor(t.Context(), tx, testScope, testAuthor)

			return err
		}))

		replies, err := store.ListReplies(t.Context(), testScope, testTarget, root.ID, nil)
		must.NoError(t, err)
		must.SliceLen(t, 1, replies.Data)
		test.EqOp(t, child.ID, replies.Data[0].ID)
	})

	t.Run("a subject who wrote nothing is not a failure", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		var deleted int64

		must.NoError(t, store.client.WithTransaction(t.Context(), func(tx database.Tx) error {
			var err error
			deleted, err = store.DeleteCommentsByAuthor(t.Context(), tx, testScope, "never_said_anything")

			return err
		}))

		test.EqOp(t, int64(0), deleted)
	})

	t.Run("a delete outside a transaction is refused", func(t *testing.T) {
		t.Parallel()

		// Both deletes run inside somebody else's transaction — the sweep in the
		// one that removes the target, the erasure in the one that removes the
		// person — so neither reaches for the store's own writer.
		store := env.newStore(t)

		_, err := store.DeleteCommentsByAuthor(t.Context(), nil, testScope, testAuthor)
		must.ErrorIs(t, err, ErrNilExecutor)

		_, err = store.DeleteCommentsForTarget(t.Context(), nil, testScope, testTarget)
		must.ErrorIs(t, err, ErrNilExecutor)
	})
}

// sweep runs DeleteCommentsForTarget in its own transaction and returns the
// count, since every caller of it here wants exactly that.
func sweep(t *testing.T, store *SQLStore, scope tenancy.Scope, target Target) int64 {
	t.Helper()

	var deleted int64

	must.NoError(t, store.client.WithTransaction(t.Context(), func(tx database.Tx) error {
		var err error
		deleted, err = store.DeleteCommentsForTarget(t.Context(), tx, scope, target)

		return err
	}))

	return deleted
}

// ids is the ids of a page, in the order it came back.
func ids(page []*Comment) []string {
	out := make([]string, 0, len(page))
	for _, c := range page {
		out = append(out, c.ID)
	}

	return out
}

func TestNewSQLStore(T *testing.T) {
	T.Parallel()

	T.Run("refuses a nil client", func(t *testing.T) {
		t.Parallel()

		_, err := NewSQLStore(nil)
		must.ErrorIs(t, err, ErrNilDatabaseClient)
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

		store, err := NewSQLStore(env.client, WithTablePrefix("cm"))
		must.NoError(t, err)
		test.EqOp(t, "cm", store.TablePrefix())
	})

	T.Run("names the dialect it has no statements for", func(t *testing.T) {
		t.Parallel()

		_, err := commentsdbDialect(dialect.Dialect("cassandra"))
		must.ErrorIs(t, err, dialect.ErrUnsupported)
	})
}
