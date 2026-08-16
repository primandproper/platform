package webhooks_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/primandproper/platform-go/v11/cryptography/requestsigning"
	"github.com/primandproper/platform-go/v11/database"
	"github.com/primandproper/platform-go/v11/database/dialect"
	"github.com/primandproper/platform-go/v11/database/sqlite"
	"github.com/primandproper/platform-go/v11/webhooks"
	"github.com/primandproper/platform-go/v11/webhooks/migrations"
)

// Dispatch writes deliveries through the caller's transaction, so an event
// cannot survive a rolled-back state change — nor be lost by a commit that
// succeeded while the publish failed.
func ExampleDispatcher_Dispatch() {
	ctx := context.Background()

	// In a real service these come from your DI container.
	client, dispatcher := exampleWiring()

	order := struct {
		ID string `json:"id"`
	}{ID: "order-7"}

	body, err := json.Marshal(order)
	if err != nil {
		panic(err)
	}

	err = client.WithTransaction(ctx, func(q database.SQLQueryExecutor) error {
		// ... the state change that produced the event ...

		return dispatcher.Dispatch(ctx, q, &webhooks.Delivery{
			EventType: "order.updated",
			// Deliveries sharing an ordering key reach a given subscriber in
			// dispatch order, so order.updated cannot overtake order.created.
			OrderingKey: order.ID,
			Payload:     body,
		})
	})

	fmt.Println(err)
	// Output: <nil>
}

// What a subscriber does on receipt. The scheme lives in
// cryptography/requestsigning, which this package signs through; a subscriber
// verifies through the same package, so the two halves cannot drift.
func Example_verifyingADelivery() {
	secret := webhooks.Secret{Current: []byte("the shared signing key")}

	subscriber := httptest.NewServer(http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		// The exact bytes received, read before any decoding. Decoding and
		// re-encoding changes key order and whitespace, and the signature covers
		// bytes rather than meaning.
		body, err := io.ReadAll(req.Body)
		if err != nil {
			res.WriteHeader(http.StatusBadRequest)

			return
		}

		if err = requestsigning.Verify(secret, body, req.Header.Get(requestsigning.SignatureHeader)); err != nil {
			res.WriteHeader(http.StatusUnauthorized)

			return
		}

		// The delivery ID is stable across every retry and replay of one
		// delivery, so it is the key to deduplicate on.
		_ = req.Header.Get(webhooks.DeliveryIDHeader)

		res.WriteHeader(http.StatusNoContent)
	}))
	defer subscriber.Close()

	payload := []byte(`{"id":"order-7"}`)

	signature, err := requestsigning.Sign(secret, payload, time.Now())
	if err != nil {
		panic(err)
	}

	req, err := http.NewRequestWithContext(context.Background(),
		http.MethodPost, subscriber.URL, strings.NewReader(string(payload)))
	if err != nil {
		panic(err)
	}

	req.Header.Set(requestsigning.SignatureHeader, signature)

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	}
	defer func() { _ = res.Body.Close() }()

	fmt.Println(res.StatusCode)

	// A tampered body no longer verifies.
	fmt.Println(requestsigning.Verify(secret, []byte(`{"id":"order-8"}`), signature))

	// Output:
	// 204
	// invalid request signature
}

// exampleWiring stands up a throwaway SQLite-backed dispatcher so the Dispatch
// example is executable rather than illustrative. A real service builds these
// once at startup, through webhooks/config.
func exampleWiring() (database.Client, webhooks.Dispatcher) {
	ctx := context.Background()

	dir, err := os.MkdirTemp("", "webhooks-example")
	if err != nil {
		panic(err)
	}

	client, err := sqlite.NewDatabaseClient(ctx, &exampleClientConfig{
		connectionString: filepath.Join(dir, "webhooks.db"),
	})
	if err != nil {
		panic(err)
	}

	stmts, err := migrations.Statements(dialect.SQLite, webhooks.DefaultTablePrefix)
	if err != nil {
		panic(err)
	}

	for _, stmt := range stmts {
		if _, err = client.Writer().ExecContext(ctx, stmt); err != nil {
			panic(err)
		}
	}

	store, err := webhooks.NewSQLStore(client)
	if err != nil {
		panic(err)
	}

	// The catalog is the application's: what its events mean is not the
	// library's opinion, and an event outside it is rejected at both ends.
	dispatcher, err := webhooks.NewDispatcher(store, webhooks.WithCatalog(webhooks.Catalog{
		"order.created": {Description: "an order was created"},
		"order.updated": {Description: "an order was updated"},
	}))
	if err != nil {
		panic(err)
	}

	return client, dispatcher
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
