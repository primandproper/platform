package settings_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/primandproper/platform-go/v14/database"
	"github.com/primandproper/platform-go/v14/database/dialect"
	"github.com/primandproper/platform-go/v14/database/sqlite"
	"github.com/primandproper/platform-go/v14/pointer"
	"github.com/primandproper/platform-go/v14/settings"
	"github.com/primandproper/platform-go/v14/settings/migrations"
	"github.com/primandproper/platform-go/v14/tenancy"
)

// Example shows the flow this package exists for: an administrator defines a
// setting, a person answers it, and the request path reads the answer back with
// the default standing in for everyone who has not.
func Example() {
	ctx := context.Background()
	store := exampleWiring()

	// One catalog, so the scope is global — the shape a single-tenant
	// application has, behaving exactly as it would without the column.
	scope := tenancy.Global()

	// The administrative half. Written once, by a migration or an admin console,
	// rather than on a request path.
	definition, err := store.CreateDefinition(ctx, scope, &settings.Definition{
		Name:        "notifications.digest",
		Description: "how often a digest email is sent",
		Kind:        settings.KindString,
		Default:     pointer.To("weekly"),
		Enumeration: []string{"daily", "weekly", "never"},
	})
	if err != nil {
		panic(err)
	}

	ada := settings.Subject{Type: settings.SubjectUser, ID: "user-ada"}
	grace := settings.Subject{Type: settings.SubjectUser, ID: "user-grace"}

	// The request path. The value is checked against the definition inside the
	// write, so a setting can only hold what it admits.
	if _, err = store.SetValue(ctx, scope, ada, definition.Name, "daily"); err != nil {
		panic(err)
	}

	if _, err = store.SetValue(ctx, scope, ada, definition.Name, "hourly"); err != nil {
		fmt.Println("refused:", errors.Is(err, settings.ErrNotEnumerated))
	}

	// Reading it back. Ada chose; Grace did not and gets the default.
	subjects := []settings.Subject{ada, grace}
	for i := range subjects {
		subject := subjects[i]

		resolved, resolveErr := store.Resolve(ctx, scope, subject, definition.Name)
		if resolveErr != nil {
			panic(resolveErr)
		}

		digest, digestErr := resolved.String()
		if digestErr != nil {
			panic(digestErr)
		}

		fmt.Printf("%s: %s (%s)\n", subject.ID, digest, resolved.Source)
	}

	// Output:
	// refused: true
	// user-ada: daily (subject)
	// user-grace: weekly (default)
}

// Example_unset shows the third answer a resolution can give, and why it is a
// sentinel rather than a fallback parameter.
//
// A setting with no value and no default has not been decided by anybody, and a
// getter taking a default would answer it with whatever the caller guessed —
// leaving the caller unable to tell "nobody has said" from "somebody said this".
func Example_unset() {
	ctx := context.Background()
	store := exampleWiring()
	scope := tenancy.Global()

	if _, err := store.CreateDefinition(ctx, scope, &settings.Definition{
		Name: "retention.days",
		Kind: settings.KindInt,
	}); err != nil {
		panic(err)
	}

	resolved, err := store.Resolve(ctx, scope,
		settings.Subject{Type: settings.SubjectAccount, ID: "account-1"}, "retention.days")
	if err != nil {
		panic(err)
	}

	switch days, readErr := resolved.Int(); {
	case errors.Is(readErr, settings.ErrSettingUnset):
		fmt.Println("nobody has decided; the caller's own policy applies")
	case readErr != nil:
		panic(readErr)
	default:
		fmt.Println("retain for", days, "days")
	}

	// Output:
	// nobody has decided; the caller's own policy applies
}

// A preference change is rarely the only row a write produces. SetValueTx puts
// it in the transaction that carries its companions — here an audit entry naming
// who changed what — so neither can land without the other.
func ExampleStore_SetValueTx() {
	ctx := context.Background()
	client := exampleDatabase(ctx)
	scope := tenancy.Global()

	// The consumer's own table, standing in for whatever a real application
	// writes beside a setting: an audit entry, a data change event on an outbox.
	if _, err := client.Writer().ExecContext(ctx,
		`CREATE TABLE audit_log (subject_id TEXT NOT NULL, setting TEXT NOT NULL, value TEXT NOT NULL)`); err != nil {
		panic(err)
	}

	store, err := settings.NewSQLStore(client)
	if err != nil {
		panic(err)
	}

	if _, err = store.CreateDefinition(ctx, scope, &settings.Definition{
		Name:        "notifications.digest",
		Kind:        settings.KindString,
		Enumeration: []string{"daily", "weekly", "never"},
	}); err != nil {
		panic(err)
	}

	ada := settings.Subject{Type: settings.SubjectUser, ID: "user-ada"}

	if err = client.WithTransaction(ctx, func(tx database.Tx) error {
		value, txErr := store.SetValueTx(ctx, tx, scope, ada, "notifications.digest", "daily")
		if txErr != nil {
			return txErr
		}

		// A failure here takes the value back with it, which is the whole
		// reason this is one transaction rather than two.
		_, txErr = tx.ExecContext(ctx,
			`INSERT INTO audit_log (subject_id, setting, value) VALUES (?, ?, ?)`,
			value.Subject.ID, "notifications.digest", value.Raw)

		return txErr
	}); err != nil {
		panic(err)
	}

	var audited string
	if err = client.Reader().QueryRowContext(ctx,
		`SELECT value FROM audit_log WHERE subject_id = ?`, ada.ID).Scan(&audited); err != nil {
		panic(err)
	}

	fmt.Println("audited:", audited)

	// Output:
	// audited: daily
}

// exampleWiring builds a throwaway SQLite-backed store. A real application hands
// migrations.SQL to its own migration run and builds the store over the database
// it already has.
func exampleWiring() settings.Store {
	store, err := settings.NewSQLStore(exampleDatabase(context.Background()))
	if err != nil {
		panic(err)
	}

	return store
}

// exampleDatabase is a throwaway SQLite database with the settings tables in it.
func exampleDatabase(ctx context.Context) database.Client {
	dir, err := os.MkdirTemp("", "settings-example")
	if err != nil {
		panic(err)
	}

	client, err := sqlite.NewDatabaseClient(ctx, &exampleClientConfig{
		connectionString: filepath.Join(dir, "settings.db"),
	})
	if err != nil {
		panic(err)
	}

	stmts, err := migrations.Statements(dialect.SQLite, settings.DefaultTablePrefix)
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
