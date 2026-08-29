package waitlists_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/primandproper/platform-go/v13/database"
	"github.com/primandproper/platform-go/v13/database/dialect"
	"github.com/primandproper/platform-go/v13/database/sqlite"
	"github.com/primandproper/platform-go/v13/tenancy"
	"github.com/primandproper/platform-go/v13/waitlists"
	"github.com/primandproper/platform-go/v13/waitlists/migrations"
)

// Example shows the flow this package exists for: a list is opened, somebody
// joins it from a form, and they are invited when it is their turn.
func Example() {
	ctx := context.Background()
	store := exampleWiring()

	// One catalog, so the scope is global — the shape a single-tenant
	// application has, behaving exactly as it would without the column.
	scope := tenancy.Global()

	// The administrative half. Written once, by whoever decides the launch is
	// happening, rather than on a request path.
	list, err := store.CreateList(ctx, scope, &waitlists.List{
		Name:        "Launch",
		Description: "early access to the beta",
		ClosesAt:    time.Now().Add(30 * 24 * time.Hour),
	})
	if err != nil {
		panic(err)
	}

	// The request path. The contact is stored as it was given and digested as
	// Normalize renders it, so two capitalizations of one address are one
	// person.
	signup, err := store.Join(ctx, scope, list.ID, &waitlists.Signup{
		Contact: "Ada@Example.com",
		Notes:   "asked about the API",
	})
	if err != nil {
		panic(err)
	}

	fmt.Println("joined as:", signup.Status)

	if _, err = store.Join(ctx, scope, list.ID, &waitlists.Signup{Contact: "ada@example.com"}); err != nil {
		fmt.Println("already on the list:", errors.Is(err, waitlists.ErrAlreadySignedUp))
	}

	// When it is their turn. The move is a guarded write, so a second
	// invitation loses rather than sending a second email.
	if err = store.Invite(ctx, scope, list.ID, signup.ID); err != nil {
		panic(err)
	}

	if err = store.Invite(ctx, scope, list.ID, signup.ID); err != nil {
		fmt.Println("invited once:", errors.Is(err, waitlists.ErrWrongStatus))
	}

	invited, err := store.GetSignup(ctx, scope, list.ID, signup.ID)
	if err != nil {
		panic(err)
	}

	fmt.Println("now:", invited.Status)

	// Output:
	// joined as: waiting
	// already on the list: true
	// invited once: true
	// now: invited
}

// Example_withdrawal shows the obligation this package is shaped around: an
// address that asks to come off a list stays off it, and the row that remembers
// that no longer holds the address.
func Example_withdrawal() {
	ctx := context.Background()
	store := exampleWiring()
	scope := tenancy.Global()

	list, err := store.CreateList(ctx, scope, &waitlists.List{
		Name:     "Launch",
		ClosesAt: time.Now().Add(30 * 24 * time.Hour),
	})
	if err != nil {
		panic(err)
	}

	signup, err := store.Join(ctx, scope, list.ID, &waitlists.Signup{Contact: "ada@example.com"})
	if err != nil {
		panic(err)
	}

	if err = store.Withdraw(ctx, scope, list.ID, signup.ID); err != nil {
		panic(err)
	}

	// What is left is the digest and the fact of the withdrawal. The address,
	// the notes and the subject reference are gone.
	withdrawn, err := store.GetSignupByContact(ctx, scope, list.ID, "ada@example.com")
	if err != nil {
		panic(err)
	}

	fmt.Printf("status %s, contact %q\n", withdrawn.Status, withdrawn.Contact)

	// And a later signup from the same address is refused rather than quietly
	// re-subscribing somebody who asked to be left alone.
	if _, err = store.Join(ctx, scope, list.ID, &waitlists.Signup{Contact: "ADA@example.com"}); err != nil {
		fmt.Println("stays off the list:", errors.Is(err, waitlists.ErrContactWithdrawn))
	}

	// Output:
	// status withdrawn, contact ""
	// stays off the list: true
}

// exampleWiring builds a throwaway SQLite-backed store. A real application hands
// migrations.SQL to its own migration run and builds the store over the database
// it already has.
func exampleWiring() waitlists.Store {
	ctx := context.Background()

	dir, err := os.MkdirTemp("", "waitlists-example")
	if err != nil {
		panic(err)
	}

	client, err := sqlite.NewDatabaseClient(ctx, &exampleClientConfig{
		connectionString: filepath.Join(dir, "waitlists.db"),
	})
	if err != nil {
		panic(err)
	}

	stmts, err := migrations.Statements(dialect.SQLite, waitlists.DefaultTablePrefix)
	if err != nil {
		panic(err)
	}

	for _, stmt := range stmts {
		if _, err = client.Writer().ExecContext(ctx, stmt); err != nil {
			panic(err)
		}
	}

	store, err := waitlists.NewSQLStore(client)
	if err != nil {
		panic(err)
	}

	return store
}

// exampleClientConfig is the minimum database.ClientConfig a SQLite client
// needs.
type exampleClientConfig struct {
	connectionString string
}

var _ database.ClientConfig = (*exampleClientConfig)(nil)

func (c *exampleClientConfig) GetReadConnectionString() string   { return c.connectionString }
func (c *exampleClientConfig) GetWriteConnectionString() string  { return c.connectionString }
func (c *exampleClientConfig) GetMaxPingAttempts() uint64        { return 1 }
func (c *exampleClientConfig) GetPingWaitPeriod() time.Duration  { return time.Millisecond }
func (c *exampleClientConfig) GetMaxIdleConns() int              { return 2 }
func (c *exampleClientConfig) GetMaxOpenConns() int              { return 1 }
func (c *exampleClientConfig) GetConnMaxLifetime() time.Duration { return time.Minute }
