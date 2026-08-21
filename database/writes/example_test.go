package writes_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/primandproper/platform-go/v12/database"
	"github.com/primandproper/platform-go/v12/database/dialect"
	"github.com/primandproper/platform-go/v12/database/sqlite"
	"github.com/primandproper/platform-go/v12/database/writes"
	"github.com/primandproper/platform-go/v12/tenancy"
)

// The shape a repository write collapses to: one generated call, the check that
// it matched something, and the identity of what it touched. The transaction,
// the audit entry, and the rollback when either fails are the Writer's.
func Example() {
	ctx := context.Background()

	client, err := exampleClient(ctx)
	if err != nil {
		panic(err)
	}

	defer func() { _ = client.Close() }()

	// One hook, registered once, for every write in the application. It appends
	// through the executor it is handed, so its row is a statement in the same
	// transaction as the row it describes.
	writer, err := writes.New(client, writes.WithHook(func(ctx context.Context, exec database.SQLQueryExecutor, change *writes.Change) error {
		_, execErr := exec.ExecContext(ctx,
			"INSERT INTO audit_log (id, resource, row_id, operation, scope) VALUES (?, ?, ?, ?, ?)",
			string(change.Op)+"_"+change.ID, change.Resource, change.ID, string(change.Op), change.Scope)

		return execErr
	}))
	if err != nil {
		panic(err)
	}

	archive := func(id string) error {
		return writer.Do(ctx, func(ctx context.Context, exec database.SQLQueryExecutor) ([]writes.Change, error) {
			result, execErr := exec.ExecContext(ctx,
				"UPDATE widgets SET archived_at = CURRENT_TIMESTAMP WHERE id = ? AND archived_at IS NULL", id)
			if execErr != nil {
				return nil, execErr
			}

			if execErr = writes.RequireAffectedResult(result); execErr != nil {
				return nil, execErr
			}

			return []writes.Change{{
				Resource: "widget",
				Table:    "widgets",
				ID:       id,
				Op:       writes.OpArchived,
				Scope:    tenancy.Global(),
			}}, nil
		})
	}

	if _, err = client.Writer().ExecContext(ctx, "INSERT INTO widgets (id, name) VALUES (?, ?)", "widget_1", "sprocket"); err != nil {
		panic(err)
	}

	fmt.Println("first archive:", archive("widget_1"))

	// The second one matches nothing, so it reports ErrNoRowsAffected — which
	// wraps sql.ErrNoRows, and therefore reaches a service's error mapper as
	// the 404 it is rather than as a 500.
	fmt.Println("second archive:", archive("widget_1"))

	// One entry, for the archive that happened. The write that matched nothing
	// rolled back, so no hook of its ran.
	var entries int
	if err = client.Reader().QueryRowContext(ctx, "SELECT COUNT(*) FROM audit_log").Scan(&entries); err != nil {
		panic(err)
	}

	fmt.Println("audit entries:", entries)

	// Output:
	// first archive: <nil>
	// second archive: no rows affected: sql: no rows in result set
	// audit entries: 1
}

// exampleClient builds a throwaway SQLite database with the two tables above.
func exampleClient(ctx context.Context) (database.Client, error) {
	dir, err := os.MkdirTemp("", "writes-example")
	if err != nil {
		return nil, err
	}

	client, err := sqlite.NewDatabaseClient(ctx, &exampleConfig{connectionString: filepath.Join(dir, "writes.db")})
	if err != nil {
		return nil, err
	}

	for _, statement := range dialect.SplitStatements(schema) {
		if _, err = client.Writer().ExecContext(ctx, statement); err != nil {
			return nil, errors.Join(err, client.Close())
		}
	}

	return client, nil
}

type exampleConfig struct {
	connectionString string
}

func (c *exampleConfig) GetReadConnectionString() string   { return c.connectionString }
func (c *exampleConfig) GetWriteConnectionString() string  { return c.connectionString }
func (c *exampleConfig) GetMaxPingAttempts() uint64        { return 1 }
func (c *exampleConfig) GetPingWaitPeriod() time.Duration  { return time.Millisecond }
func (c *exampleConfig) GetMaxIdleConns() int              { return 2 }
func (c *exampleConfig) GetMaxOpenConns() int              { return 1 }
func (c *exampleConfig) GetConnMaxLifetime() time.Duration { return time.Minute }
