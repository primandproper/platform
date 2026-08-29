package comments_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/primandproper/platform-go/v13/comments"
	"github.com/primandproper/platform-go/v13/comments/migrations"
	"github.com/primandproper/platform-go/v13/database"
	"github.com/primandproper/platform-go/v13/database/dialect"
	"github.com/primandproper/platform-go/v13/database/sqlite"
	"github.com/primandproper/platform-go/v13/tenancy"
)

// The application's own vocabulary, declared as constants so the catalog below
// and every call site agree by type rather than by spelling.
const (
	recipeTarget comments.TargetType = "recipe"
	mealTarget   comments.TargetType = "meal"
)

// A discussion is one write per comment and two reads: the target's roots, then
// one root's replies.
func Example() {
	ctx := context.Background()

	store, err := comments.NewSQLStore(exampleClient(ctx),
		comments.WithTargets(comments.Targets{
			recipeTarget: {Description: "a recipe"},
			mealTarget:   {Description: "a meal"},
		}))
	if err != nil {
		panic(err)
	}

	scope := tenancy.Of("acct_1")
	recipe := comments.Target{Type: recipeTarget, ID: "recipe_1"}

	root := &comments.Comment{
		Scope:  scope,
		Target: recipe,
		Author: "user_1",
		Body:   "halved the sugar and it was still too sweet",
	}
	if err = store.CreateComment(ctx, root); err != nil {
		panic(err)
	}

	// A reply names its parent and nothing else about where it goes: its target
	// is its parent's, and the store fills it in.
	answer := &comments.Comment{
		Scope:    scope,
		ParentID: root.ID,
		Author:   "user_2",
		Body:     "try two thirds of the syrup as well",
	}
	if err = store.CreateComment(ctx, answer); err != nil {
		panic(err)
	}

	fmt.Println("reply is about:", answer.Target.Type, answer.Target.ID)

	// The top of the discussion. The count beside the page is of every root on
	// the target rather than of the page, so a client asking for ten still knows
	// how many there are.
	roots, err := store.ListRootComments(ctx, scope, recipe, nil)
	if err != nil {
		panic(err)
	}

	fmt.Println("roots:", roots.FilteredCount)

	replies, err := store.ListReplies(ctx, scope, recipe, root.ID, nil)
	if err != nil {
		panic(err)
	}

	fmt.Println("replies to the first:", replies.FilteredCount)

	// Output:
	// reply is about: recipe recipe_1
	// roots: 1
	// replies to the first: 1
}

// The catalog is what stops a comment being written where nothing will list it.
// A target type nobody registered is refused at the write rather than stored and
// discovered as an absence.
func ExampleWithTargets() {
	ctx := context.Background()

	store, err := comments.NewSQLStore(exampleClient(ctx),
		comments.WithTargets(comments.Targets{
			recipeTarget: {Description: "a recipe"},
		}))
	if err != nil {
		panic(err)
	}

	misspelled := &comments.Comment{
		Scope:  tenancy.Of("acct_1"),
		Target: comments.Target{Type: "recipies", ID: "recipe_1"},
		Author: "user_1",
		Body:   "this would have been stored under a type nothing lists",
	}

	fmt.Println("refused:", store.CreateComment(ctx, misspelled) != nil)
	fmt.Println("what can be commented on:", store.TargetTypes())

	// Output:
	// refused: true
	// what can be commented on: [recipe]
}

// A comment outlives the thing it is about, and the consumer's delete is what
// removes it. The sweep runs in the transaction that removes the target, so
// there is no window in which one is gone and the other is not.
func ExampleStore_DeleteCommentsForTarget() {
	ctx := context.Background()

	client := exampleClient(ctx)

	store, err := comments.NewSQLStore(client,
		comments.WithTargets(comments.Targets{recipeTarget: {Description: "a recipe"}}))
	if err != nil {
		panic(err)
	}

	scope := tenancy.Of("acct_1")
	recipe := comments.Target{Type: recipeTarget, ID: "recipe_1"}

	root := &comments.Comment{
		Scope: scope, Target: recipe, Author: "user_1", Body: "lovely",
	}
	if err = store.CreateComment(ctx, root); err != nil {
		panic(err)
	}

	if err = store.CreateComment(ctx, &comments.Comment{
		Scope: scope, ParentID: root.ID, Author: "user_2", Body: "agreed",
	}); err != nil {
		panic(err)
	}

	var swept int64

	// In the real thing, deleting the recipe happens in this transaction too.
	if err = client.WithTransaction(ctx, func(tx database.Tx) error {
		swept, err = store.DeleteCommentsForTarget(ctx, tx, scope, recipe)

		return err
	}); err != nil {
		panic(err)
	}

	fmt.Println("swept:", swept)

	// Output:
	// swept: 2
}

// exampleClient is a throwaway SQLite database with the comments table in it, so
// the examples above run as written.
func exampleClient(ctx context.Context) database.Client {
	dir, err := os.MkdirTemp("", "comments-example")
	if err != nil {
		panic(err)
	}

	client, err := sqlite.NewDatabaseClient(ctx,
		&exampleClientConfig{connectionString: filepath.Join(dir, "comments.db")})
	if err != nil {
		panic(err)
	}

	stmts, err := migrations.Statements(dialect.SQLite, comments.DefaultTablePrefix)
	if err != nil {
		panic(err)
	}

	for _, stmt := range stmts {
		if _, err = client.Writer().ExecContext(ctx, stmt); err != nil {
			panic(err)
		}
	}

	return client
}

type exampleClientConfig struct {
	connectionString string
}

func (c *exampleClientConfig) GetReadConnectionString() string   { return c.connectionString }
func (c *exampleClientConfig) GetWriteConnectionString() string  { return c.connectionString }
func (c *exampleClientConfig) GetMaxPingAttempts() uint64        { return 1 }
func (c *exampleClientConfig) GetPingWaitPeriod() time.Duration  { return time.Millisecond }
func (c *exampleClientConfig) GetMaxIdleConns() int              { return 2 }
func (c *exampleClientConfig) GetMaxOpenConns() int              { return 1 }
func (c *exampleClientConfig) GetConnMaxLifetime() time.Duration { return time.Minute }
