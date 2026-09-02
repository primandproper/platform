package billing_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/primandproper/platform-go/v14/billing"
	"github.com/primandproper/platform-go/v14/billing/migrations"
	"github.com/primandproper/platform-go/v14/capitalism"
	"github.com/primandproper/platform-go/v14/database"
	"github.com/primandproper/platform-go/v14/database/dialect"
	"github.com/primandproper/platform-go/v14/database/sqlite"
	"github.com/primandproper/platform-go/v14/tenancy"
)

// Example shows the flow this package exists for: a catalog is stocked, a
// customer's subscription arrives from a payment provider, and the charge that
// renewed it is recorded in the ledger.
func Example() {
	ctx := context.Background()
	store := exampleWiring()

	// One catalog, so the scope is global — the shape a single-tenant
	// application has, behaving exactly as it would without the column.
	scope := tenancy.Global()

	// The administrative half. Written once, by whoever decides what is on
	// offer, rather than on a request path.
	product, err := store.CreateProduct(ctx, scope, &billing.Product{
		Name:                  "Pro",
		Kind:                  billing.KindRecurring,
		AmountCents:           2_500,
		Currency:              "usd", // upper-cased on write
		BillingIntervalMonths: 1,
		ExternalProductID:     "prod_abc",
	})
	if err != nil {
		panic(err)
	}

	fmt.Println("selling:", product.Name, product.Currency, product.AmountCents)

	// The webhook half. What the provider reports is stored as reported; nothing
	// here decides what the status means.
	subscription, err := store.CreateSubscription(ctx, scope, &billing.Subscription{
		BelongsToAccount:       "account-1",
		ProductID:              product.ID,
		ExternalSubscriptionID: "sub_abc",
		Status:                 capitalism.SubscriptionStatusActive,
		CurrentPeriodStart:     time.Now().Add(-24 * time.Hour),
		CurrentPeriodEnd:       time.Now().Add(30 * 24 * time.Hour),
	})
	if err != nil {
		panic(err)
	}

	// The ledger. The provider's own identifier is unique within the scope, so
	// the second delivery of one charge collides instead of recording it twice.
	charge := &billing.Transaction{
		BelongsToAccount:      "account-1",
		SubscriptionID:        subscription.ID,
		ExternalTransactionID: "ch_abc",
		Status:                billing.TransactionSucceeded,
		AmountCents:           2_500,
		Currency:              "USD",
	}

	if _, err = store.RecordTransaction(ctx, scope, charge); err != nil {
		panic(err)
	}

	_, err = store.RecordTransaction(ctx, scope, &billing.Transaction{
		BelongsToAccount:      "account-1",
		SubscriptionID:        subscription.ID,
		ExternalTransactionID: "ch_abc",
		Status:                billing.TransactionSucceeded,
		AmountCents:           2_500,
		Currency:              "USD",
	})
	fmt.Println("redelivery recorded once:", errors.Is(err, billing.ErrTransactionExists))

	ledger, err := store.ListTransactionsForAccount(ctx, scope, "account-1", nil)
	if err != nil {
		panic(err)
	}

	fmt.Println("ledger rows:", len(ledger.Data))

	// Output:
	// selling: Pro USD 2500
	// redelivery recorded once: true
	// ledger rows: 1
}

// Example_entitlement shows the seam this package fills and the one it
// deliberately does not.
//
// Which subscriptions cover this instant is a fact, and this store answers it.
// Which of their statuses leaves an account entitled is policy, and stays with
// the caller — see billing/plans, which is the same reading as a
// entitlements.PlanSource.
func Example_entitlement() {
	ctx := context.Background()
	store := exampleWiring()
	scope := tenancy.Global()

	product, err := store.CreateProduct(ctx, scope, &billing.Product{
		Name:                  "Pro",
		Kind:                  billing.KindRecurring,
		AmountCents:           2_500,
		Currency:              "USD",
		BillingIntervalMonths: 1,
	})
	if err != nil {
		panic(err)
	}

	subscription, err := store.CreateSubscription(ctx, scope, &billing.Subscription{
		BelongsToAccount:   "account-1",
		ProductID:          product.ID,
		Status:             capitalism.SubscriptionStatusPastDue,
		CurrentPeriodStart: time.Now().Add(-24 * time.Hour),
		CurrentPeriodEnd:   time.Now().Add(7 * 24 * time.Hour),
	})
	if err != nil {
		panic(err)
	}

	current, err := store.ListCurrentSubscriptions(ctx, scope, "account-1", nil)
	if err != nil {
		panic(err)
	}

	fmt.Println("inside its paid period:", len(current.Data) == 1)
	fmt.Println("what the processor says:", subscription.Status)

	// The judgement, which is the caller's. Keeping a past_due customer working
	// through the dunning window is a decision a deployment makes; this package
	// stores the fact it is made from.
	entitled := subscription.Status == capitalism.SubscriptionStatusActive ||
		subscription.Status == capitalism.SubscriptionStatusPastDue

	fmt.Println("our rule says entitled:", entitled)

	// Output:
	// inside its paid period: true
	// what the processor says: past_due
	// our rule says entitled: true
}

// Example_redeliveredStatus shows the guard every provider-driven write carries:
// the second delivery of one event writes nothing and says so.
func Example_redeliveredStatus() {
	ctx := context.Background()
	store := exampleWiring()
	scope := tenancy.Global()

	product, err := store.CreateProduct(ctx, scope, &billing.Product{
		Name:                  "Pro",
		Kind:                  billing.KindRecurring,
		AmountCents:           2_500,
		Currency:              "USD",
		BillingIntervalMonths: 1,
	})
	if err != nil {
		panic(err)
	}

	subscription, err := store.CreateSubscription(ctx, scope, &billing.Subscription{
		BelongsToAccount:   "account-1",
		ProductID:          product.ID,
		Status:             capitalism.SubscriptionStatusActive,
		CurrentPeriodStart: time.Now().Add(-24 * time.Hour),
		CurrentPeriodEnd:   time.Now().Add(7 * 24 * time.Hour),
	})
	if err != nil {
		panic(err)
	}

	if err = store.SetSubscriptionStatus(ctx, scope, subscription.ID,
		capitalism.SubscriptionStatusCanceled); err != nil {
		panic(err)
	}

	err = store.SetSubscriptionStatus(ctx, scope, subscription.ID,
		capitalism.SubscriptionStatusCanceled)

	// An answer rather than a failure: the work the event describes has already
	// been done, and a handler acknowledges the delivery on it.
	fmt.Println("already recorded:", errors.Is(err, billing.ErrStatusUnchanged))

	// A write against a subscription nobody has is a different thing entirely,
	// and is reported as one.
	err = store.SetSubscriptionStatus(ctx, scope, "sub-nobody-has",
		capitalism.SubscriptionStatusCanceled)
	fmt.Println("no such subscription:", errors.Is(err, billing.ErrSubscriptionNotFound))

	// Output:
	// already recorded: true
	// no such subscription: true
}

// exampleWiring builds a store over a throwaway SQLite database.
func exampleWiring() billing.Store {
	ctx := context.Background()

	dir, err := os.MkdirTemp("", "billing-example")
	if err != nil {
		panic(err)
	}

	client, err := sqlite.NewDatabaseClient(ctx, &exampleClientConfig{
		connectionString: filepath.Join(dir, "billing.db"),
	})
	if err != nil {
		panic(err)
	}

	stmts, err := migrations.Statements(dialect.SQLite, billing.DefaultTablePrefix)
	if err != nil {
		panic(err)
	}

	for _, stmt := range stmts {
		if _, err = client.Writer().ExecContext(ctx, stmt); err != nil {
			panic(err)
		}
	}

	store, err := billing.NewSQLStore(client)
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
