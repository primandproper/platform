package notifications_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/primandproper/platform-go/v13/database"
	"github.com/primandproper/platform-go/v13/database/dialect"
	"github.com/primandproper/platform-go/v13/database/sqlite"
	"github.com/primandproper/platform-go/v13/notifications"
	"github.com/primandproper/platform-go/v13/notifications/migrations"
	"github.com/primandproper/platform-go/v13/notifications/mobile"
	"github.com/primandproper/platform-go/v13/tenancy"
)

// Telling somebody something is two writes: the inbox row they will see next
// time they open the app, and the push to whatever handsets they have
// registered.
func Example() {
	ctx := context.Background()

	store, err := notifications.NewSQLStore(exampleClient(ctx))
	if err != nil {
		panic(err)
	}

	scope := tenancy.Of("acct_1")

	// The inbox row. Topic is the application's own category; the store stores
	// it and never interprets it.
	notification := &notifications.Notification{
		Scope:     scope,
		Principal: "user_1",
		Topic:     "order.shipped",
		Title:     "Your order shipped",
		Body:      "Arriving Thursday.",
		Link:      "/orders/1",
	}
	if err = store.CreateNotification(ctx, notification); err != nil {
		panic(err)
	}

	// The handsets. A device registers itself on every app launch, and the write
	// converges on the token, so this is the same call whether it is the first
	// registration or the thousandth.
	if err = store.RegisterDevice(ctx, &notifications.Device{
		Scope:     scope,
		Principal: "user_1",
		Platform:  notifications.PlatformIOS,
		Token:     "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	}); err != nil {
		panic(err)
	}

	devices, err := store.ListDevicesByPrincipals(ctx, scope, []string{"user_1"})
	if err != nil {
		panic(err)
	}

	fmt.Println("devices to push to:", len(devices))

	unread, err := store.ListUnreadNotifications(ctx, scope, "user_1", nil)
	if err != nil {
		panic(err)
	}

	fmt.Println("badge count:", unread.FilteredCount)

	// Output:
	// devices to push to: 1
	// badge count: 1
}

// Wiring the store into a sender is what closes the loop the registry exists
// for: a provider rejecting a token permanently is what removes the row, and a
// registry nothing tells keeps addressing pushes to handsets that no longer
// exist.
func ExampleRegistry_InvalidateDeviceToken() {
	ctx := context.Background()

	store, err := notifications.NewSQLStore(exampleClient(ctx))
	if err != nil {
		panic(err)
	}

	scope := tenancy.Of("acct_1")

	if err = store.RegisterDevice(ctx, &notifications.Device{
		Scope:     scope,
		Principal: "user_1",
		Platform:  notifications.PlatformAndroid,
		Token:     "a-token-the-app-was-uninstalled-from",
	}); err != nil {
		panic(err)
	}

	// What a real deployment wires, once, at startup. A send that comes back
	// UNREGISTERED then prunes the row on its way out.
	_ = mobile.NewMultiPlatformPushSender(nil, nil, mobile.WithTokenInvalidator(store))

	// Standing in for that send here, because reaching FCM would need
	// credentials: this is the call the sender makes.
	if err = store.InvalidateDeviceToken(ctx, "android", "a-token-the-app-was-uninstalled-from"); err != nil {
		panic(err)
	}

	devices, err := store.ListDevicesByPrincipals(ctx, scope, []string{"user_1"})
	if err != nil {
		panic(err)
	}

	fmt.Println("devices left:", len(devices))

	// Output:
	// devices left: 0
}

// exampleClient is a throwaway SQLite database with the notifications tables in
// it, so the examples above run as written.
func exampleClient(ctx context.Context) database.Client {
	dir, err := os.MkdirTemp("", "notifications-example")
	if err != nil {
		panic(err)
	}

	client, err := sqlite.NewDatabaseClient(ctx,
		&exampleClientConfig{connectionString: filepath.Join(dir, "notifications.db")})
	if err != nil {
		panic(err)
	}

	stmts, err := migrations.Statements(dialect.SQLite, notifications.DefaultTablePrefix)
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
