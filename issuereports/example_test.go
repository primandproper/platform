package issuereports_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/primandproper/platform-go/v14/database"
	"github.com/primandproper/platform-go/v14/database/dialect"
	"github.com/primandproper/platform-go/v14/database/sqlite"
	"github.com/primandproper/platform-go/v14/issuereports"
	"github.com/primandproper/platform-go/v14/issuereports/migrations"
	"github.com/primandproper/platform-go/v14/tenancy"
)

// Taking a report is one write, and working the queue is one read plus one
// guarded move.
func Example() {
	ctx := context.Background()

	store, err := issuereports.NewSQLStore(exampleClient(ctx))
	if err != nil {
		panic(err)
	}

	scope := tenancy.Of("acct_1")

	// What the form collected. Kind is the application's own category; the store
	// stores it and never interprets it, and the subject is how the application
	// names the thing being reported on.
	report := &issuereports.Report{
		Scope:       scope,
		Reporter:    "user_1",
		Kind:        "bug",
		Details:     "the save button does nothing on the recipe editor",
		SubjectType: "recipes",
		SubjectID:   "recipe_1",
	}
	if err = store.CreateReport(ctx, report); err != nil {
		panic(err)
	}

	fmt.Println("filed as:", report.Status)

	// The triage queue. The count beside the page is of everything open rather
	// than of the page, so a console asking for ten reports still knows how many
	// there are.
	queue, err := store.ListReportsByStatus(ctx, scope, issuereports.StatusOpen, nil)
	if err != nil {
		panic(err)
	}

	fmt.Println("open reports:", queue.FilteredCount)

	// Working it. The status the triager read goes over with the one they are
	// moving to, so a second triager holding the same view loses rather than
	// silently overwriting this note.
	resolved, err := store.TransitionReport(ctx, scope, report.ID,
		issuereports.StatusOpen, issuereports.StatusResolved, "fixed in 1.4")
	if err != nil {
		panic(err)
	}

	fmt.Println("resolved:", resolved.Closed(), "-", resolved.Resolution)

	// Output:
	// filed as: open
	// open reports: 1
	// resolved: true - fixed in 1.4
}

// The guard is what makes the queue safe for more than one triager. The second
// of two people acting on the same view of a report is told so rather than
// overwriting the first one's decision.
func ExampleStore_TransitionReport() {
	ctx := context.Background()

	store, err := issuereports.NewSQLStore(exampleClient(ctx))
	if err != nil {
		panic(err)
	}

	scope := tenancy.Of("acct_1")

	report := &issuereports.Report{
		Scope:    scope,
		Reporter: "user_1",
		Kind:     "billing",
		Details:  "charged twice for one order",
	}
	if err = store.CreateReport(ctx, report); err != nil {
		panic(err)
	}

	// Both triagers read the report while it was open.
	if _, err = store.TransitionReport(ctx, scope, report.ID,
		issuereports.StatusOpen, issuereports.StatusResolved, "refunded"); err != nil {
		panic(err)
	}

	_, err = store.TransitionReport(ctx, scope, report.ID,
		issuereports.StatusOpen, issuereports.StatusDeclined, "duplicate")

	fmt.Println("second triager:", err != nil)

	current, err := store.GetReport(ctx, scope, report.ID)
	if err != nil {
		panic(err)
	}

	fmt.Println("still says:", current.Status, "-", current.Resolution)

	// Output:
	// second triager: true
	// still says: resolved - refunded
}

// exampleClient is a throwaway SQLite database with the issue reports table in
// it, so the examples above run as written.
func exampleClient(ctx context.Context) database.Client {
	dir, err := os.MkdirTemp("", "issuereports-example")
	if err != nil {
		panic(err)
	}

	client, err := sqlite.NewDatabaseClient(ctx,
		&exampleClientConfig{connectionString: filepath.Join(dir, "issuereports.db")})
	if err != nil {
		panic(err)
	}

	stmts, err := migrations.Statements(dialect.SQLite, issuereports.DefaultTablePrefix)
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
