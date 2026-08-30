package billing

import (
	"sync"
	"testing"
	"time"

	"github.com/primandproper/platform-go/v13/capitalism"
	"github.com/primandproper/platform-go/v13/database/dialect"
	platformerrors "github.com/primandproper/platform-go/v13/errors"
	"github.com/primandproper/platform-go/v13/tenancy"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// TestSQLStore_SQLite runs the behavioral suite against SQLite, which needs no
// container. TestSQLStore_RealServers runs the same suite against Postgres and
// MySQL.
func TestSQLStore_SQLite(T *testing.T) {
	T.Parallel()

	runStoreSuite(T, newSQLiteEnv(T))
}

// runStoreSuite is every behavior this store promises, against whatever database
// the environment holds.
//
// It is one function rather than one per dialect because the promises are the
// same on all three — that is what billing/internal/billingdb is for — and a
// suite written per dialect is a suite where the dialect that matters least gets
// the assertions the others do not.
func runStoreSuite(t *testing.T, env *storeEnv) {
	t.Helper()

	t.Run("products", func(t *testing.T) {
		t.Parallel()
		runProductSuite(t, env)
	})

	t.Run("subscriptions", func(t *testing.T) {
		t.Parallel()
		runSubscriptionSuite(t, env)
	})

	t.Run("purchases", func(t *testing.T) {
		t.Parallel()
		runPurchaseSuite(t, env)
	})

	t.Run("transactions", func(t *testing.T) {
		t.Parallel()
		runTransactionSuite(t, env)
	})

	t.Run("create guards", func(t *testing.T) {
		t.Parallel()
		runCreateGuardSuite(t, env)
	})

	t.Run("administrative", func(t *testing.T) {
		t.Parallel()
		runAdministrativeSuite(t, env)
	})

	t.Run("paging", func(t *testing.T) {
		t.Parallel()
		runPagingSuite(t, env)
	})
}

// runCreateGuardSuite is what the four creates promise when the row does not go
// in, which is one group rather than four because one statement shape makes all
// of those promises.
//
// Every create here is an insert-ignore keyed on the table's (scope, external id)
// unique index, so a collision is an affected count rather than a driver error
// and there is no window between deciding the identifier is free and using it.
// What the count cannot say is what was collided with, and these are the
// assertions that the store says it correctly — including on the two mistakes it
// has to read to tell apart from a redelivery.
func runCreateGuardSuite(t *testing.T, env *storeEnv) {
	t.Helper()

	t.Run("reports one winner when a provider redelivers a charge concurrently", func(t *testing.T) {
		t.Parallel()

		const racers = 8

		// Its own environment, because the suite's pool opens one connection and
		// eight writers through one connection take turns. See concurrentEnv for
		// why SQLite has none to give.
		concurrent, ok := env.concurrentEnv(t, racers)
		if !ok {
			t.Skip("this engine has no concurrent writers to race")
		}

		store := concurrent.newStore(t)

		product := mustCreateProduct(t, store, testScope, oneTimeProduct("guide"))
		purchase := mustCreatePurchase(t, store, testScope, outstandingPurchase(product.ID, testAccount))

		// What the ledger promises when the same charge arrives more than once at
		// the same time: one row, and every loser told so by the sentinel it
		// documents rather than by a driver's constraint violation.
		//
		// It asserts the contract rather than reproducing its absence. The shape
		// this replaced decided the identifier was free in a statement before the
		// one that used it, which is a window by construction — but not one this
		// harness can open: the racers reach RecordTransaction together and the
		// pool still hands them the server one at a time, so every loser here
		// would find the row committed and report the same sentinel. The reason
		// to believe the fix is that there is no longer a second statement to
		// cross, and the reason to keep the test is that a future shape which
		// reintroduces one has somewhere to fail.

		var (
			start   = make(chan struct{})
			results = make(chan error, racers)
			wg      sync.WaitGroup
		)

		for range racers {
			wg.Go(func() {
				attempt := pendingTransaction(testAccount)
				attempt.PurchaseID = purchase.ID
				attempt.ExternalTransactionID = "ch_redelivered"

				<-start

				_, err := store.RecordTransaction(t.Context(), testScope, attempt)
				results <- err
			})
		}

		close(start)
		wg.Wait()
		close(results)

		var won int

		for err := range results {
			if err == nil {
				won++

				continue
			}

			test.ErrorIs(t, err, ErrTransactionExists)
		}

		test.EqOp(t, 1, won)

		// And the ledger holds what the winners wrote, which is the reason any of
		// this matters: a sum over these rows is a number nobody reconciles by
		// hand.
		page, err := store.ListTransactionsForAccount(t.Context(), testScope, testAccount, nil)
		must.NoError(t, err)
		test.SliceLen(t, 1, page.Data)
	})

	t.Run("refuses a subscription naming a product nobody has", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		_, err := store.CreateSubscription(t.Context(), testScope,
			currentSubscription("no-such-product", testAccount))
		test.ErrorIs(t, err, ErrProductNotFound)
	})

	t.Run("refuses a purchase naming a product nobody has", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		_, err := store.CreatePurchase(t.Context(), testScope,
			outstandingPurchase("no-such-product", testAccount))
		test.ErrorIs(t, err, ErrProductNotFound)
	})

	t.Run("refuses a subscription naming a product in another scope", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		// The existence check is scoped, so a product that exists is still not a
		// product this scope may sell. Without that the foreign key would admit
		// the row, since the key spans ids and knows nothing about tenancy.
		product := mustCreateProduct(t, store, otherScope, recurringProduct("pro"))

		_, err := store.CreateSubscription(t.Context(), testScope,
			currentSubscription(product.ID, testAccount))
		test.ErrorIs(t, err, ErrProductNotFound)
	})

	t.Run("lets a ledger row name an archived subscription", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		product := mustCreateProduct(t, store, testScope, recurringProduct("pro"))
		subscription := mustCreateSubscription(t, store, testScope,
			currentSubscription(product.ID, testAccount))

		must.NoError(t, store.ArchiveSubscription(t.Context(), testScope, subscription.ID))

		// Archiving is administrative and deliberately leaves the ledger alone,
		// so the presence check behind a losing insert is archived-blind. A check
		// that read only live rows would refuse the refund of something since
		// retired.
		attempt := pendingTransaction(testAccount)
		attempt.SubscriptionID = subscription.ID

		mustRecordTransaction(t, store, testScope, attempt)
	})

	t.Run("refuses a ledger row naming a subscription nobody has", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		attempt := pendingTransaction(testAccount)
		attempt.SubscriptionID = "no-such-subscription"

		_, err := store.RecordTransaction(t.Context(), testScope, attempt)
		must.Error(t, err)

		// All three engines refuse the row and they differ in which error says
		// so. Postgres and SQLite raise the foreign key at the insert; MySQL's
		// IGNORE downgrades it to a warning and a zero affected count, which is
		// the count a collision produces — so there the store reads to tell the
		// two apart, and names the missing subscription. Asserting the sentinel
		// on the engines that never reach that read would assert somebody else's
		// error message.
		if env.dialect == dialect.MySQL {
			test.ErrorIs(t, err, ErrSubscriptionNotFound)
		}
	})

	t.Run("refuses a create whose id another row already carries", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		first := recurringProduct("pro")
		first.ID = "product-chosen-by-hand"
		mustCreateProduct(t, store, testScope, first)

		second := recurringProduct("pro-annual")
		second.ID = "product-chosen-by-hand"

		_, err := store.CreateProduct(t.Context(), testScope, second)
		must.Error(t, err)

		// Reachable only from a caller that supplies its own id, and reported by
		// the two engines whose IGNORE covers every constraint on the table.
		// Postgres absorbs only the index its ON CONFLICT names, so the primary
		// key raises there instead — see ErrIDTaken.
		if env.dialect != dialect.Postgres {
			test.ErrorIs(t, err, ErrIDTaken)
		}
	})
}

func runProductSuite(t *testing.T, env *storeEnv) {
	t.Helper()

	t.Run("round trips a recurring product", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		created := mustCreateProduct(t, store, testScope, recurringProduct("pro"))
		test.NotEq(t, "", created.ID)
		test.False(t, created.CreatedAt.IsZero())
		test.EqOp(t, testScope, created.Scope)

		read, err := store.GetProduct(t.Context(), testScope, created.ID)
		must.NoError(t, err)
		test.EqOp(t, created.ID, read.ID)
		test.EqOp(t, "pro", read.Name)
		test.EqOp(t, KindRecurring, read.Kind)
		test.EqOp(t, int64(2_500), read.AmountCents)
		test.EqOp(t, int64(1), read.BillingIntervalMonths)
		test.True(t, read.Recurring())
	})

	t.Run("a one-time product stores no billing interval", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		created := mustCreateProduct(t, store, testScope, oneTimeProduct("lifetime"))

		read, err := store.GetProduct(t.Context(), testScope, created.ID)
		must.NoError(t, err)
		test.EqOp(t, int64(0), read.BillingIntervalMonths)
		test.False(t, read.Recurring())
	})

	t.Run("upper-cases the currency on write", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		product := recurringProduct("pro")
		product.Currency = " usd "

		created := mustCreateProduct(t, store, testScope, product)
		test.EqOp(t, "USD", created.Currency)

		read, err := store.GetProduct(t.Context(), testScope, created.ID)
		must.NoError(t, err)
		test.EqOp(t, "USD", read.Currency)
	})

	t.Run("refuses a recurring product with no interval", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		product := recurringProduct("pro")
		product.BillingIntervalMonths = 0

		_, err := store.CreateProduct(t.Context(), testScope, product)
		test.ErrorIs(t, err, ErrEmptyBillingInterval)
	})

	t.Run("refuses a one-time product carrying an interval", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		product := oneTimeProduct("lifetime")
		product.BillingIntervalMonths = 12

		_, err := store.CreateProduct(t.Context(), testScope, product)
		test.ErrorIs(t, err, ErrUnexpectedBillingInterval)
	})

	t.Run("refuses a second product claiming one provider product", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		first := recurringProduct("pro")
		first.ExternalProductID = "prod_stripe_1"
		mustCreateProduct(t, store, testScope, first)

		second := recurringProduct("pro-annual")
		second.ExternalProductID = "prod_stripe_1"

		_, err := store.CreateProduct(t.Context(), testScope, second)
		test.ErrorIs(t, err, ErrProductExists)
	})

	t.Run("lets many products carry no provider product", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		// The whole reason the column is nullable rather than defaulted: a free
		// tier and a comped plan are both products with no provider behind them,
		// and under the empty string the second would collide with the first.
		mustCreateProduct(t, store, testScope, recurringProduct("free"))
		mustCreateProduct(t, store, testScope, recurringProduct("comped"))

		page, err := store.ListProducts(t.Context(), testScope, nil)
		must.NoError(t, err)
		test.SliceLen(t, 2, page.Data)
	})

	t.Run("keeps a provider product taken after the product is archived", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		first := recurringProduct("pro")
		first.ExternalProductID = "prod_stripe_2"
		created := mustCreateProduct(t, store, testScope, first)

		must.NoError(t, store.ArchiveProduct(t.Context(), testScope, created.ID))

		second := recurringProduct("pro-again")
		second.ExternalProductID = "prod_stripe_2"

		_, err := store.CreateProduct(t.Context(), testScope, second)
		test.ErrorIs(t, err, ErrProductExists)
	})

	t.Run("reads by provider product, and refuses an archived one", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		product := recurringProduct("pro")
		product.ExternalProductID = "prod_stripe_3"
		created := mustCreateProduct(t, store, testScope, product)

		read, err := store.GetProductByExternalID(t.Context(), testScope, "prod_stripe_3")
		must.NoError(t, err)
		test.EqOp(t, created.ID, read.ID)

		must.NoError(t, store.ArchiveProduct(t.Context(), testScope, created.ID))

		_, err = store.GetProductByExternalID(t.Context(), testScope, "prod_stripe_3")
		test.ErrorIs(t, err, ErrProductNotFound)
	})

	t.Run("refuses a lookup with no provider product", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		_, err := store.GetProductByExternalID(t.Context(), testScope, "")
		test.ErrorIs(t, err, ErrEmptyExternalID)
	})

	t.Run("lets a product keep its own provider product through an update", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		product := recurringProduct("pro")
		product.ExternalProductID = "prod_stripe_4"
		created := mustCreateProduct(t, store, testScope, product)

		created.AmountCents = 3_000
		must.NoError(t, store.UpdateProduct(t.Context(), testScope, created))

		read, err := store.GetProduct(t.Context(), testScope, created.ID)
		must.NoError(t, err)
		test.EqOp(t, int64(3_000), read.AmountCents)
		test.EqOp(t, "prod_stripe_4", read.ExternalProductID)
		test.NotNil(t, read.LastUpdatedAt)
	})

	t.Run("refuses an update claiming another product's provider product", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		first := recurringProduct("pro")
		first.ExternalProductID = "prod_stripe_5"
		mustCreateProduct(t, store, testScope, first)

		second := mustCreateProduct(t, store, testScope, recurringProduct("basic"))
		second.ExternalProductID = "prod_stripe_5"

		test.ErrorIs(t, store.UpdateProduct(t.Context(), testScope, second), ErrProductExists)
	})

	t.Run("reports existence, and stops reporting it once archived", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		created := mustCreateProduct(t, store, testScope, recurringProduct("pro"))

		exists, err := store.ProductExists(t.Context(), testScope, created.ID)
		must.NoError(t, err)
		test.True(t, exists)

		must.NoError(t, store.ArchiveProduct(t.Context(), testScope, created.ID))

		exists, err = store.ProductExists(t.Context(), testScope, created.ID)
		must.NoError(t, err)
		test.False(t, exists)
	})

	t.Run("does not reach another scope's catalog", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		created := mustCreateProduct(t, store, testScope, recurringProduct("pro"))

		_, err := store.GetProduct(t.Context(), otherScope, created.ID)
		test.ErrorIs(t, err, ErrProductNotFound)

		test.ErrorIs(t, store.ArchiveProduct(t.Context(), otherScope, created.ID), ErrProductNotFound)

		page, err := store.ListProducts(t.Context(), otherScope, nil)
		must.NoError(t, err)
		test.SliceEmpty(t, page.Data)
	})

	t.Run("refuses an unscoped call", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		_, err := store.CreateProduct(t.Context(), tenancy.Scope{}, recurringProduct("pro"))
		test.Error(t, err)
	})
}

func runSubscriptionSuite(t *testing.T, env *storeEnv) {
	t.Helper()

	t.Run("round trips an agreement", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		product := mustCreateProduct(t, store, testScope, recurringProduct("pro"))

		created := mustCreateSubscription(t, store, testScope, currentSubscription(product.ID, testAccount))

		read, err := store.GetSubscription(t.Context(), testScope, created.ID)
		must.NoError(t, err)
		test.EqOp(t, testAccount, read.BelongsToAccount)
		test.EqOp(t, product.ID, read.ProductID)
		test.EqOp(t, capitalism.SubscriptionStatusActive, read.Status)
		test.True(t, read.CurrentAt(testNow))
	})

	t.Run("refuses a status capitalism does not recognize", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		product := mustCreateProduct(t, store, testScope, recurringProduct("pro"))

		subscription := currentSubscription(product.ID, testAccount)
		subscription.Status = capitalism.SubscriptionStatusUnknown

		_, err := store.CreateSubscription(t.Context(), testScope, subscription)
		test.ErrorIs(t, err, ErrInvalidStatus)
	})

	t.Run("refuses a period that ends before it starts", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		product := mustCreateProduct(t, store, testScope, recurringProduct("pro"))

		subscription := currentSubscription(product.ID, testAccount)
		subscription.CurrentPeriodEnd = subscription.CurrentPeriodStart.Add(-time.Hour)

		_, err := store.CreateSubscription(t.Context(), testScope, subscription)
		test.ErrorIs(t, err, ErrBackwardsPeriod)
	})

	t.Run("refuses a redelivered subscription", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		product := mustCreateProduct(t, store, testScope, recurringProduct("pro"))

		first := currentSubscription(product.ID, testAccount)
		first.ExternalSubscriptionID = "sub_stripe_1"
		mustCreateSubscription(t, store, testScope, first)

		second := currentSubscription(product.ID, testAccount)
		second.ExternalSubscriptionID = "sub_stripe_1"

		_, err := store.CreateSubscription(t.Context(), testScope, second)
		test.ErrorIs(t, err, ErrSubscriptionExists)
	})

	t.Run("lets many agreements be granted by hand", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		product := mustCreateProduct(t, store, testScope, recurringProduct("pro"))

		mustCreateSubscription(t, store, testScope, currentSubscription(product.ID, testAccount))
		mustCreateSubscription(t, store, testScope, currentSubscription(product.ID, otherAccount))

		page, err := store.ListSubscriptions(t.Context(), testScope, nil)
		must.NoError(t, err)
		test.SliceLen(t, 2, page.Data)
	})

	t.Run("reads by provider subscription", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		product := mustCreateProduct(t, store, testScope, recurringProduct("pro"))

		subscription := currentSubscription(product.ID, testAccount)
		subscription.ExternalSubscriptionID = "sub_stripe_2"
		created := mustCreateSubscription(t, store, testScope, subscription)

		read, err := store.GetSubscriptionByExternalID(t.Context(), testScope, "sub_stripe_2")
		must.NoError(t, err)
		test.EqOp(t, created.ID, read.ID)
	})

	t.Run("pages one account's agreements and not another's", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		product := mustCreateProduct(t, store, testScope, recurringProduct("pro"))

		mine := mustCreateSubscription(t, store, testScope, currentSubscription(product.ID, testAccount))
		mustCreateSubscription(t, store, testScope, currentSubscription(product.ID, otherAccount))

		page, err := store.ListSubscriptionsForAccount(t.Context(), testScope, testAccount, nil)
		must.NoError(t, err)
		must.SliceLen(t, 1, page.Data)
		test.EqOp(t, mine.ID, page.Data[0].ID)
	})

	t.Run("refuses an account-keyed read with no account", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		_, err := store.ListSubscriptionsForAccount(t.Context(), testScope, "", nil)
		test.ErrorIs(t, err, ErrEmptyAccount)
	})

	t.Run("pages only the agreements whose period covers now", func(t *testing.T) {
		t.Parallel()

		store, stub := env.newStoreWithClock(t)
		product := mustCreateProduct(t, store, testScope, recurringProduct("pro"))

		current := mustCreateSubscription(t, store, testScope, currentSubscription(product.ID, testAccount))
		mustCreateSubscription(t, store, testScope, lapsedSubscription(product.ID, testAccount))

		page, err := store.ListCurrentSubscriptions(t.Context(), testScope, testAccount, nil)
		must.NoError(t, err)
		must.SliceLen(t, 1, page.Data)
		test.EqOp(t, current.ID, page.Data[0].ID)

		// The horizon is the store's clock rather than the server's, which is
		// the whole reason it is a bound argument: moving the clock past the
		// period end takes the agreement off this page.
		stub.advance(60 * 24 * time.Hour)

		page, err = store.ListCurrentSubscriptions(t.Context(), testScope, testAccount, nil)
		must.NoError(t, err)
		test.SliceEmpty(t, page.Data)
	})

	t.Run("syncs the plan, the standing and the period together", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		product := mustCreateProduct(t, store, testScope, recurringProduct("pro"))
		upgrade := mustCreateProduct(t, store, testScope, recurringProduct("enterprise"))

		created := mustCreateSubscription(t, store, testScope, currentSubscription(product.ID, testAccount))

		created.ProductID = upgrade.ID
		created.Status = capitalism.SubscriptionStatusPastDue
		created.CurrentPeriodEnd = testNow.Add(60 * 24 * time.Hour)

		must.NoError(t, store.UpdateSubscription(t.Context(), testScope, created))

		read, err := store.GetSubscription(t.Context(), testScope, created.ID)
		must.NoError(t, err)
		test.EqOp(t, upgrade.ID, read.ProductID)
		test.EqOp(t, capitalism.SubscriptionStatusPastDue, read.Status)
	})

	t.Run("moves the standing once, and tells a redelivery so", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		product := mustCreateProduct(t, store, testScope, recurringProduct("pro"))
		created := mustCreateSubscription(t, store, testScope, currentSubscription(product.ID, testAccount))

		must.NoError(t, store.SetSubscriptionStatus(t.Context(), testScope, created.ID,
			capitalism.SubscriptionStatusPastDue))

		read, err := store.GetSubscription(t.Context(), testScope, created.ID)
		must.NoError(t, err)
		test.EqOp(t, capitalism.SubscriptionStatusPastDue, read.Status)

		// The guard is `SET status = X WHERE status <> X`, so the second
		// delivery of one event writes nothing and says which of the two
		// refusals it is.
		err = store.SetSubscriptionStatus(t.Context(), testScope, created.ID,
			capitalism.SubscriptionStatusPastDue)
		test.ErrorIs(t, err, ErrStatusUnchanged)
	})

	t.Run("tells a missing subscription from a redelivery", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		err := store.SetSubscriptionStatus(t.Context(), testScope, "nope",
			capitalism.SubscriptionStatusActive)
		test.ErrorIs(t, err, ErrSubscriptionNotFound)
	})

	t.Run("refuses a status write in another scope", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		product := mustCreateProduct(t, store, testScope, recurringProduct("pro"))
		created := mustCreateSubscription(t, store, testScope, currentSubscription(product.ID, testAccount))

		err := store.SetSubscriptionStatus(t.Context(), otherScope, created.ID,
			capitalism.SubscriptionStatusCanceled)
		test.ErrorIs(t, err, ErrSubscriptionNotFound)
	})

	t.Run("archives without touching the ledger", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		product := mustCreateProduct(t, store, testScope, recurringProduct("pro"))
		created := mustCreateSubscription(t, store, testScope, currentSubscription(product.ID, testAccount))

		ledger := pendingTransaction(testAccount)
		ledger.SubscriptionID = created.ID
		recorded := mustRecordTransaction(t, store, testScope, ledger)

		must.NoError(t, store.ArchiveSubscription(t.Context(), testScope, created.ID))

		_, err := store.GetSubscription(t.Context(), testScope, created.ID)
		test.ErrorIs(t, err, ErrSubscriptionNotFound)

		stillThere, err := store.GetTransaction(t.Context(), testScope, recorded.ID)
		must.NoError(t, err)
		test.EqOp(t, created.ID, stillThere.SubscriptionID)
	})
}

func runPurchaseSuite(t *testing.T, env *storeEnv) {
	t.Helper()

	t.Run("round trips an outstanding sale", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		product := mustCreateProduct(t, store, testScope, oneTimeProduct("lifetime"))

		created := mustCreatePurchase(t, store, testScope, outstandingPurchase(product.ID, testAccount))
		test.False(t, created.Complete())

		read, err := store.GetPurchase(t.Context(), testScope, created.ID)
		must.NoError(t, err)
		test.Nil(t, read.CompletedAt)
		test.EqOp(t, int64(999), read.AmountCents)
	})

	t.Run("completes once, at the provider's own moment", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		product := mustCreateProduct(t, store, testScope, oneTimeProduct("lifetime"))
		created := mustCreatePurchase(t, store, testScope, outstandingPurchase(product.ID, testAccount))

		// Deliberately older than the store's clock: a webhook arrives after the
		// payment it describes, and revenue is recognized against when the money
		// moved.
		settled := testNow.Add(-2 * time.Hour)

		must.NoError(t, store.CompletePurchase(t.Context(), testScope, created.ID, settled))

		read, err := store.GetPurchase(t.Context(), testScope, created.ID)
		must.NoError(t, err)
		must.NotNil(t, read.CompletedAt)
		test.True(t, read.Complete())
		test.True(t, settled.Equal(read.CompletedAt.UTC()))

		test.ErrorIs(t,
			store.CompletePurchase(t.Context(), testScope, created.ID, settled),
			ErrAlreadyCompleted)
	})

	t.Run("falls back to the store's clock for a completion with no provider", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		product := mustCreateProduct(t, store, testScope, oneTimeProduct("lifetime"))
		created := mustCreatePurchase(t, store, testScope, outstandingPurchase(product.ID, testAccount))

		must.NoError(t, store.CompletePurchase(t.Context(), testScope, created.ID, time.Time{}))

		read, err := store.GetPurchase(t.Context(), testScope, created.ID)
		must.NoError(t, err)
		must.NotNil(t, read.CompletedAt)
		test.True(t, testNow.Equal(read.CompletedAt.UTC()))
	})

	t.Run("reports a missing purchase rather than a replay", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		test.ErrorIs(t,
			store.CompletePurchase(t.Context(), testScope, "nope", testNow),
			ErrPurchaseNotFound)
	})

	t.Run("refuses a redelivered sale", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		product := mustCreateProduct(t, store, testScope, oneTimeProduct("lifetime"))

		first := outstandingPurchase(product.ID, testAccount)
		first.ExternalTransactionID = "pi_stripe_1"
		mustCreatePurchase(t, store, testScope, first)

		second := outstandingPurchase(product.ID, testAccount)
		second.ExternalTransactionID = "pi_stripe_1"

		_, err := store.CreatePurchase(t.Context(), testScope, second)
		test.ErrorIs(t, err, ErrPurchaseExists)
	})

	t.Run("pages one account's sales and not another's", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		product := mustCreateProduct(t, store, testScope, oneTimeProduct("lifetime"))

		mine := mustCreatePurchase(t, store, testScope, outstandingPurchase(product.ID, testAccount))
		mustCreatePurchase(t, store, testScope, outstandingPurchase(product.ID, otherAccount))

		page, err := store.ListPurchasesForAccount(t.Context(), testScope, testAccount, nil)
		must.NoError(t, err)
		must.SliceLen(t, 1, page.Data)
		test.EqOp(t, mine.ID, page.Data[0].ID)

		everything, err := store.ListPurchases(t.Context(), testScope, nil)
		must.NoError(t, err)
		test.SliceLen(t, 2, everything.Data)
	})
}

func runTransactionSuite(t *testing.T, env *storeEnv) {
	t.Helper()

	t.Run("round trips a ledger row", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		recorded := mustRecordTransaction(t, store, testScope, pendingTransaction(testAccount))
		test.False(t, recorded.CreatedAt.IsZero())

		read, err := store.GetTransaction(t.Context(), testScope, recorded.ID)
		must.NoError(t, err)
		test.EqOp(t, TransactionPending, read.Status)
		test.EqOp(t, "", read.SubscriptionID)
		test.EqOp(t, "", read.PurchaseID)
	})

	t.Run("records one charge once, however many times it is delivered", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		first := pendingTransaction(testAccount)
		first.ExternalTransactionID = "ch_stripe_1"
		mustRecordTransaction(t, store, testScope, first)

		second := pendingTransaction(testAccount)
		second.ExternalTransactionID = "ch_stripe_1"

		_, err := store.RecordTransaction(t.Context(), testScope, second)
		test.ErrorIs(t, err, ErrTransactionExists)

		page, err := store.ListTransactions(t.Context(), testScope, nil)
		must.NoError(t, err)
		test.SliceLen(t, 1, page.Data)
	})

	t.Run("finds a redelivery before it writes", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		transaction := pendingTransaction(testAccount)
		transaction.ExternalTransactionID = "ch_stripe_2"
		recorded := mustRecordTransaction(t, store, testScope, transaction)

		read, err := store.GetTransactionByExternalID(t.Context(), testScope, "ch_stripe_2")
		must.NoError(t, err)
		test.EqOp(t, recorded.ID, read.ID)

		_, err = store.GetTransactionByExternalID(t.Context(), testScope, "ch_stripe_unseen")
		test.ErrorIs(t, err, ErrTransactionNotFound)
	})

	t.Run("refuses a row claiming both a subscription and a purchase", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		product := mustCreateProduct(t, store, testScope, recurringProduct("pro"))
		subscription := mustCreateSubscription(t, store, testScope, currentSubscription(product.ID, testAccount))
		purchase := mustCreatePurchase(t, store, testScope, outstandingPurchase(product.ID, testAccount))

		transaction := pendingTransaction(testAccount)
		transaction.SubscriptionID = subscription.ID
		transaction.PurchaseID = purchase.ID

		_, err := store.RecordTransaction(t.Context(), testScope, transaction)
		test.ErrorIs(t, err, ErrAmbiguousTransaction)
	})

	t.Run("moves an outcome once, and tells a redelivery so", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		recorded := mustRecordTransaction(t, store, testScope, pendingTransaction(testAccount))

		must.NoError(t, store.SetTransactionStatus(t.Context(), testScope, recorded.ID, TransactionSucceeded))

		read, err := store.GetTransaction(t.Context(), testScope, recorded.ID)
		must.NoError(t, err)
		test.EqOp(t, TransactionSucceeded, read.Status)

		test.ErrorIs(t,
			store.SetTransactionStatus(t.Context(), testScope, recorded.ID, TransactionSucceeded),
			ErrStatusUnchanged)
	})

	t.Run("refuses a status this package does not implement", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		recorded := mustRecordTransaction(t, store, testScope, pendingTransaction(testAccount))

		test.ErrorIs(t,
			store.SetTransactionStatus(t.Context(), testScope, recorded.ID, TransactionStatus("settled")),
			ErrInvalidStatus)
	})

	t.Run("records a refund as its own positive row", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		charge := mustRecordTransaction(t, store, testScope, pendingTransaction(testAccount))
		must.NoError(t, store.SetTransactionStatus(t.Context(), testScope, charge.ID, TransactionSucceeded))

		refund := pendingTransaction(testAccount)
		refund.Status = TransactionRefunded
		refund.AmountCents = 500
		mustRecordTransaction(t, store, testScope, refund)

		page, err := store.ListTransactionsForAccount(t.Context(), testScope, testAccount, nil)
		must.NoError(t, err)
		must.SliceLen(t, 2, page.Data)

		for _, row := range page.Data {
			test.True(t, row.AmountCents >= 0)
		}
	})

	t.Run("refuses a negative amount", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		transaction := pendingTransaction(testAccount)
		transaction.AmountCents = -500

		_, err := store.RecordTransaction(t.Context(), testScope, transaction)
		test.ErrorIs(t, err, ErrNegativeAmount)
	})

	t.Run("does not reach another scope's ledger", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		recorded := mustRecordTransaction(t, store, testScope, pendingTransaction(testAccount))

		_, err := store.GetTransaction(t.Context(), otherScope, recorded.ID)
		test.ErrorIs(t, err, ErrTransactionNotFound)

		page, err := store.ListTransactionsForAccount(t.Context(), otherScope, testAccount, nil)
		must.NoError(t, err)
		test.SliceEmpty(t, page.Data)
	})

	t.Run("refuses a nil row", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		_, err := store.RecordTransaction(t.Context(), testScope, nil)
		test.ErrorIs(t, err, platformerrors.ErrNilInputParameter)
	})
}
