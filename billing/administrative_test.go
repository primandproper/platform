package billing

import (
	"testing"
	"time"

	"github.com/primandproper/platform-go/v14/tenancy"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// runAdministrativeSuite is the reads keyed on a provider's identifier and the
// two archives that retire a row by hand.
//
// They are grouped because they share a promise the per-entity suites do not
// make: each of these statements sees archived rows — the by-external-id read
// because it is also the collision check a write runs — and it is the Go around
// them, not the SQL, that decides what an archived row means to a caller.
func runAdministrativeSuite(t *testing.T, env *storeEnv) {
	t.Helper()

	t.Run("reads a purchase by the provider's payment identifier", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		product := mustCreateProduct(t, store, testScope, oneTimeProduct("lifetime"))

		purchase := outstandingPurchase(product.ID, testAccount)
		purchase.ExternalTransactionID = "pi_stripe_1"
		created := mustCreatePurchase(t, store, testScope, purchase)

		read, err := store.GetPurchaseByExternalID(t.Context(), testScope, "pi_stripe_1")
		must.NoError(t, err)
		test.EqOp(t, created.ID, read.ID)
	})

	t.Run("does not report an archived purchase, though the statement can see it", func(t *testing.T) {
		t.Parallel()

		// The read behind this is the write's collision check, so it returns
		// archived rows on purpose. A caller asking for a live purchase is not
		// the caller asking whether an identifier is taken.
		store := env.newStore(t)
		product := mustCreateProduct(t, store, testScope, oneTimeProduct("lifetime"))

		purchase := outstandingPurchase(product.ID, testAccount)
		purchase.ExternalTransactionID = "pi_stripe_archived"
		created := mustCreatePurchase(t, store, testScope, purchase)

		must.NoError(t, store.ArchivePurchase(t.Context(), testScope, created.ID))

		_, err := store.GetPurchaseByExternalID(t.Context(), testScope, "pi_stripe_archived")
		test.ErrorIs(t, err, ErrPurchaseNotFound)
	})

	t.Run("keeps a payment identifier taken after the purchase is archived", func(t *testing.T) {
		t.Parallel()

		// The other side of the same fact: a provider that redelivers a charge
		// for a sale somebody archived must not be able to write it again.
		store := env.newStore(t)
		product := mustCreateProduct(t, store, testScope, oneTimeProduct("lifetime"))

		first := outstandingPurchase(product.ID, testAccount)
		first.ExternalTransactionID = "pi_stripe_reused"
		created := mustCreatePurchase(t, store, testScope, first)

		must.NoError(t, store.ArchivePurchase(t.Context(), testScope, created.ID))

		second := outstandingPurchase(product.ID, testAccount)
		second.ExternalTransactionID = "pi_stripe_reused"

		_, err := store.CreatePurchase(t.Context(), testScope, second)
		test.ErrorIs(t, err, ErrPurchaseExists)
	})

	t.Run("reports a purchase nobody has", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		_, err := store.GetPurchaseByExternalID(t.Context(), testScope, "pi_nobody_has")
		test.ErrorIs(t, err, ErrPurchaseNotFound)
	})

	t.Run("refuses a purchase lookup with no payment identifier", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		_, err := store.GetPurchaseByExternalID(t.Context(), testScope, "")
		test.ErrorIs(t, err, ErrEmptyExternalID)
	})

	t.Run("refuses an unscoped purchase lookup", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		_, err := store.GetPurchaseByExternalID(t.Context(), tenancy.Scope{}, "pi_stripe_1")
		must.Error(t, err)
	})

	t.Run("does not reach another scope's sales by payment identifier", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		product := mustCreateProduct(t, store, testScope, oneTimeProduct("lifetime"))

		purchase := outstandingPurchase(product.ID, testAccount)
		purchase.ExternalTransactionID = "pi_stripe_scoped"
		mustCreatePurchase(t, store, testScope, purchase)

		_, err := store.GetPurchaseByExternalID(t.Context(), otherScope, "pi_stripe_scoped")
		test.ErrorIs(t, err, ErrPurchaseNotFound)
	})

	t.Run("archives a purchase without touching the ledger", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		product := mustCreateProduct(t, store, testScope, oneTimeProduct("lifetime"))
		created := mustCreatePurchase(t, store, testScope, outstandingPurchase(product.ID, testAccount))

		ledger := pendingTransaction(testAccount)
		ledger.PurchaseID = created.ID
		recorded := mustRecordTransaction(t, store, testScope, ledger)

		must.NoError(t, store.ArchivePurchase(t.Context(), testScope, created.ID))

		_, err := store.GetPurchase(t.Context(), testScope, created.ID)
		test.ErrorIs(t, err, ErrPurchaseNotFound)

		// The money that moved is not undone by retiring the sale it paid for.
		stillThere, err := store.GetTransaction(t.Context(), testScope, recorded.ID)
		must.NoError(t, err)
		test.EqOp(t, created.ID, stillThere.PurchaseID)
	})

	t.Run("reports a purchase archive nobody can act on", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		test.ErrorIs(t, store.ArchivePurchase(t.Context(), testScope, "purchase-nobody-has"), ErrPurchaseNotFound)
	})

	t.Run("refuses an unscoped purchase archive", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		must.Error(t, store.ArchivePurchase(t.Context(), tenancy.Scope{}, "purchase-1"))
	})

	t.Run("does not archive another scope's sale", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		product := mustCreateProduct(t, store, testScope, oneTimeProduct("lifetime"))
		created := mustCreatePurchase(t, store, testScope, outstandingPurchase(product.ID, testAccount))

		test.ErrorIs(t, store.ArchivePurchase(t.Context(), otherScope, created.ID), ErrPurchaseNotFound)

		survivor, err := store.GetPurchase(t.Context(), testScope, created.ID)
		must.NoError(t, err)
		test.EqOp(t, created.ID, survivor.ID)
	})

	t.Run("archives a ledger row", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		recorded := mustRecordTransaction(t, store, testScope, pendingTransaction(testAccount))

		must.NoError(t, store.ArchiveTransaction(t.Context(), testScope, recorded.ID))

		_, err := store.GetTransaction(t.Context(), testScope, recorded.ID)
		test.ErrorIs(t, err, ErrTransactionNotFound)
	})

	t.Run("reports a ledger archive nobody can act on", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		test.ErrorIs(t,
			store.ArchiveTransaction(t.Context(), testScope, "transaction-nobody-has"),
			ErrTransactionNotFound)
	})

	t.Run("refuses an unscoped ledger archive", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		must.Error(t, store.ArchiveTransaction(t.Context(), tenancy.Scope{}, "transaction-1"))
	})

	t.Run("does not archive another scope's ledger row", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		recorded := mustRecordTransaction(t, store, testScope, pendingTransaction(testAccount))

		test.ErrorIs(t, store.ArchiveTransaction(t.Context(), otherScope, recorded.ID), ErrTransactionNotFound)

		survivor, err := store.GetTransaction(t.Context(), testScope, recorded.ID)
		must.NoError(t, err)
		test.EqOp(t, recorded.ID, survivor.ID)
	})

	t.Run("lets a subscription keep its own provider subscription through an update", func(t *testing.T) {
		t.Parallel()

		// The update asks whether the identifier is free, and the row that
		// already holds it is not a collision with itself.
		store := env.newStore(t)
		product := mustCreateProduct(t, store, testScope, recurringProduct("pro"))

		subscription := currentSubscription(product.ID, testAccount)
		subscription.ExternalSubscriptionID = "sub_stripe_kept"
		created := mustCreateSubscription(t, store, testScope, subscription)

		created.CurrentPeriodEnd = created.CurrentPeriodEnd.Add(24 * time.Hour)

		must.NoError(t, store.UpdateSubscription(t.Context(), testScope, created))
	})

	t.Run("refuses an update claiming another subscription's provider subscription", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		product := mustCreateProduct(t, store, testScope, recurringProduct("pro"))

		first := currentSubscription(product.ID, testAccount)
		first.ExternalSubscriptionID = "sub_stripe_taken"
		mustCreateSubscription(t, store, testScope, first)

		second := currentSubscription(product.ID, otherAccount)
		created := mustCreateSubscription(t, store, testScope, second)

		created.ExternalSubscriptionID = "sub_stripe_taken"

		test.ErrorIs(t, store.UpdateSubscription(t.Context(), testScope, created), ErrSubscriptionExists)
	})

	t.Run("an update with no provider subscription is always free", func(t *testing.T) {
		t.Parallel()

		// An empty identifier is stored as NULL and NULL repeats, which is what
		// makes an agreement granted by hand storable at all — so several of
		// them can be updated without ever colliding.
		store := env.newStore(t)
		product := mustCreateProduct(t, store, testScope, recurringProduct("pro"))

		first := mustCreateSubscription(t, store, testScope, currentSubscription(product.ID, testAccount))
		second := mustCreateSubscription(t, store, testScope, currentSubscription(product.ID, otherAccount))

		must.NoError(t, store.UpdateSubscription(t.Context(), testScope, first))
		must.NoError(t, store.UpdateSubscription(t.Context(), testScope, second))
	})
}
