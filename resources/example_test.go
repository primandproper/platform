package resources_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/primandproper/platform-go/v12/database"
	"github.com/primandproper/platform-go/v12/database/dialect"
	"github.com/primandproper/platform-go/v12/database/sqlite"
	"github.com/primandproper/platform-go/v12/filtering"
	"github.com/primandproper/platform-go/v12/resources"
	"github.com/primandproper/platform-go/v12/tenancy"
)

// Signup is one row of a waitlist: a person, an invite code, and the three
// timestamps every table here has.
type Signup struct {
	CreatedAt     time.Time
	LastUpdatedAt *time.Time
	ArchivedAt    *time.Time
	ID            string
	Email         string
	InviteCode    string
	BelongsToUser string
	Scope         tenancy.Scope
}

// signups is the declaration. It is the migration's column list, plus which of
// those columns are the id, the tenancy scope, and the author.
var signups = resources.MustDefine(dialect.SQLite, resources.Definition[Signup]{
	Name:  "signup",
	Table: "signups",

	Columns: []resources.Column[Signup]{
		resources.ID(func(s *Signup) *string { return &s.ID }),
		resources.Scope("scope", func(s *Signup) *tenancy.Scope { return &s.Scope }),
		resources.Field("email", func(s *Signup) *string { return &s.Email }).Immutable(),
		resources.Field("invite_code", func(s *Signup) *string { return &s.InviteCode }),
		resources.Owner("belongs_to_user", func(s *Signup) *string { return &s.BelongsToUser }, resources.OwnerWrites),
		resources.Field("created_at", func(s *Signup) *time.Time { return &s.CreatedAt }),
		resources.Field("last_updated_at", func(s *Signup) **time.Time { return &s.LastUpdatedAt }),
		resources.Field("archived_at", func(s *Signup) **time.Time { return &s.ArchivedAt }),
	},

	// The one keyed read this resource answers, and the moment to ask whether
	// the index behind it exists.
	Lookups: []resources.Lookup{resources.On("invite_code")},
})

// signupStore is what the service depending on this resource declares, on its
// own side: the three methods it actually calls.
//
// This is the answer to "where is the resources.Store interface" — there isn't
// one, and this is why. Six lines here name exactly what the service may do, a
// fake for it has three methods, and neither grows when this package gains one.
type signupStore interface {
	Get(ctx context.Context, scope tenancy.Scope, actor resources.Actor, id string) (*Signup, error)
	List(ctx context.Context, scope tenancy.Scope, actor resources.Actor, filter *filtering.QueryFilter, matches ...resources.Match) (*filtering.QueryFilteredResult[Signup], error)
	Create(ctx context.Context, scope tenancy.Scope, actor resources.Actor, row *Signup) (*Signup, error)
}

// Example shows the whole of a domain's data layer: a declaration, a store over
// it, and the consumer-side interface a service depends on.
func Example() {
	ctx := context.Background()

	client := exampleClient(ctx)
	defer func() { _ = client.Close() }()

	store, err := resources.NewStore(signups, client)
	if err != nil {
		panic(err)
	}

	// The service takes the narrow interface; the store satisfies it without
	// being asked to.
	var data signupStore = store

	acme := tenancy.Of("account_acme")
	alice := resources.ActingAs("user_alice")

	created, err := data.Create(ctx, acme, alice, &Signup{
		Email:         "alice@example.com",
		InviteCode:    "EARLYBIRD",
		BelongsToUser: "user_alice",
	})
	if err != nil {
		panic(err)
	}

	// The scope was written from the argument rather than trusted from the row,
	// and created_at came back from the server.
	fmt.Println(created.Scope.Owner(), created.Email, !created.CreatedAt.IsZero())

	page, err := data.List(ctx, acme, alice, nil, resources.By("invite_code", "EARLYBIRD"))
	if err != nil {
		panic(err)
	}

	fmt.Println(len(page.Data), page.FilteredCount)

	// Another tenant's read of the same code is a different set of rows, because
	// every read this package issues carries the scope.
	other, err := data.List(ctx, tenancy.Of("account_other"), alice, nil, resources.By("invite_code", "EARLYBIRD"))
	if err != nil {
		panic(err)
	}

	fmt.Println(len(other.Data))

	// Output:
	// account_acme alice@example.com true
	// 1 1
	// 0
}

// exampleClient stands in for the database.Client a DI container would provide.
func exampleClient(ctx context.Context) database.Client {
	dir, err := os.MkdirTemp("", "resources-example")
	if err != nil {
		panic(err)
	}

	client, err := sqlite.NewDatabaseClient(ctx,
		&testClientConfig{connectionString: filepath.Join(dir, "signups.db")})
	if err != nil {
		panic(err)
	}

	if _, err = client.Writer().ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS signups (
    id TEXT NOT NULL PRIMARY KEY,
    scope TEXT NOT NULL,
    email TEXT NOT NULL,
    invite_code TEXT NOT NULL,
    belongs_to_user TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_updated_at DATETIME,
    archived_at DATETIME
);`); err != nil {
		panic(err)
	}

	return client
}
