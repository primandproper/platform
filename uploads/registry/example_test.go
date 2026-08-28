package registry_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/primandproper/platform-go/v13/database"
	"github.com/primandproper/platform-go/v13/database/dialect"
	"github.com/primandproper/platform-go/v13/database/sqlite"
	"github.com/primandproper/platform-go/v13/tenancy"
	"github.com/primandproper/platform-go/v13/uploads"
	"github.com/primandproper/platform-go/v13/uploads/objectstorage"
	"github.com/primandproper/platform-go/v13/uploads/registry"
	"github.com/primandproper/platform-go/v13/uploads/registry/migrations"
)

// Example shows the flow the package exists for: bytes into storage, a row
// beside them, and an access decision made from the row rather than from the
// bucket.
func Example() {
	ctx := context.Background()
	manager, store := exampleWiring()

	// One tenant, so the scope is global — the shape a single-tenant
	// application has, behaving exactly as it would without the column.
	scope := tenancy.Global()

	object := &registry.Object{
		Scope:       scope,
		Key:         "avatars/ada/original.png",
		ContentType: "image/png",
		OwnerID:     "user_ada",
		BelongsTo:   registry.Subject{Type: "user", ID: "user_ada"},
	}

	// StoreAndRecord writes the bytes, then the row, and fills in the size from
	// what actually went past.
	if err := registry.StoreAndRecord(ctx, manager, store, object,
		strings.NewReader("\x89PNG not really")); err != nil {
		panic(err)
	}

	fmt.Println("size:", object.Size)

	// Later, a request arrives holding the key rather than the row id. The row
	// is what says whether this caller may have the bytes.
	found, err := store.GetObjectByKey(ctx, scope, "avatars/ada/original.png")
	switch {
	case errors.Is(err, registry.ErrObjectNotFound):
		fmt.Println("no such object in this tenant")
	case err != nil:
		panic(err)
	default:
		fmt.Println("owner:", found.OwnerID)
		fmt.Println("may user_bob read it:", found.OwnerID == "user_bob")
	}

	// Archiving is metadata-only: the row is hidden and the object stays in the
	// bucket until the consumer's retention policy removes it.
	if err = store.ArchiveObject(ctx, scope, object.ID); err != nil {
		panic(err)
	}

	_, err = store.GetObjectByKey(ctx, scope, "avatars/ada/original.png")
	fmt.Println("archived reads as absent:", errors.Is(err, registry.ErrObjectNotFound))

	// The object is still there.
	exists, err := manager.Exists(ctx, "avatars/ada/original.png")
	if err != nil {
		panic(err)
	}

	fmt.Println("bytes still in the bucket:", exists)

	// Output:
	// size: 15
	// owner: user_ada
	// may user_bob read it: false
	// archived reads as absent: true
	// bytes still in the bucket: true
}

// ExampleStore_listObjectsBySubject shows the page a consumer serves when it is
// asked what is attached to one of its own rows.
func ExampleStore_listObjectsBySubject() {
	ctx := context.Background()
	manager, store := exampleWiring()

	scope := tenancy.Global()
	invoice := registry.Subject{Type: "invoice", ID: "invoice_2026_01"}

	for _, name := range []string{"january-receipt.pdf", "january-statement.pdf"} {
		object := &registry.Object{
			Scope:       scope,
			Key:         "invoices/" + name,
			ContentType: "application/pdf",
			OwnerID:     "user_ada",
			BelongsTo:   invoice,
		}

		if err := registry.StoreAndRecord(ctx, manager, store, object, strings.NewReader(name)); err != nil {
			panic(err)
		}
	}

	page, err := store.ListObjectsBySubject(ctx, scope, invoice, nil)
	if err != nil {
		panic(err)
	}

	for _, o := range page.Data {
		fmt.Println(o.Key)
	}

	// Output:
	// invoices/january-receipt.pdf
	// invoices/january-statement.pdf
}

// exampleWiring stands up an in-memory bucket and a SQLite-backed registry over
// a temporary file. A real deployment migrates the table through its own
// migration run — see uploads/registry/migrations.
func exampleWiring() (uploads.UploadManager, registry.Store) {
	ctx := context.Background()

	dir, err := os.MkdirTemp("", "uploads-registry-example")
	if err != nil {
		panic(err)
	}

	manager, err := objectstorage.NewUploadManager(ctx,
		&objectstorage.Config{Provider: objectstorage.MemoryProvider, BucketName: "example"})
	if err != nil {
		panic(err)
	}

	client, err := sqlite.NewDatabaseClient(ctx, &exampleClientConfig{
		connectionString: filepath.Join(dir, "registry.db"),
	})
	if err != nil {
		panic(err)
	}

	stmts, err := migrations.Statements(dialect.SQLite, registry.DefaultTablePrefix)
	if err != nil {
		panic(err)
	}

	for _, stmt := range stmts {
		if _, err = client.Writer().ExecContext(ctx, stmt); err != nil {
			panic(err)
		}
	}

	store, err := registry.NewSQLStore(client)
	if err != nil {
		panic(err)
	}

	return manager, store
}

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
