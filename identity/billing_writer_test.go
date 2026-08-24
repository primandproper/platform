package identity

import (
	"testing"

	platformerrors "github.com/primandproper/platform-go/v13/errors"
	"github.com/primandproper/platform-go/v13/pointer"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// runBillingWriterSuite covers the one method a processor webhook reaches,
// which is why what it can and cannot write is worth stating case by case.
func runBillingWriterSuite(t *testing.T, env *storeEnv) {
	t.Helper()

	t.Run("writes only the billing fields the update names", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		owner := createUser(t, store, newUser("ada"))
		account := createAccountFor(t, store, owner, "Acme")

		must.NoError(t, store.UpdateAccountBilling(t.Context(), testScope, account.ID, &BillingUpdate{
			Status:                     pointer.To(BillingTrial),
			SubscriptionPlanID:         pointer.To("plan_pro"),
			PaymentProcessorCustomerID: pointer.To("cus_123"),
			SyncedAt:                   pointer.To(baseTime),
		}))

		full, err := store.GetAccount(t.Context(), testScope, account.ID)
		must.NoError(t, err)
		must.NotNil(t, full.SubscriptionPlanID)
		test.EqOp(t, "plan_pro", *full.SubscriptionPlanID)
		test.EqOp(t, "cus_123", full.PaymentProcessorCustomerID)
		must.NotNil(t, full.LastPaymentProviderSyncedAt)

		// A webhook carrying a status alone leaves everything else where it was,
		// which is what makes this method safe against a concurrent one.
		must.NoError(t, store.UpdateAccountBilling(t.Context(), testScope, account.ID, &BillingUpdate{
			Status: pointer.To(BillingPaid),
		}))

		partial, err := store.GetAccount(t.Context(), testScope, account.ID)
		must.NoError(t, err)
		test.EqOp(t, BillingPaid, partial.BillingStatus)
		must.NotNil(t, partial.SubscriptionPlanID)
		test.EqOp(t, "plan_pro", *partial.SubscriptionPlanID)

		// Cancelling names the plan explicitly rather than omitting it.
		must.NoError(t, store.UpdateAccountBilling(t.Context(), testScope, account.ID, &BillingUpdate{
			SubscriptionPlanID: pointer.To(""),
		}))

		cancelled, err := store.GetAccount(t.Context(), testScope, account.ID)
		must.NoError(t, err)
		test.Nil(t, cancelled.SubscriptionPlanID)
	})

	t.Run("refuses a billing update that writes nothing", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		owner := createUser(t, store, newUser("ada"))
		account := createAccountFor(t, store, owner, "Acme")

		must.ErrorIs(t,
			store.UpdateAccountBilling(t.Context(), testScope, account.ID, &BillingUpdate{}),
			platformerrors.ErrEmptyInputParameter,
		)

		must.ErrorIs(t,
			store.UpdateAccountBilling(t.Context(), testScope, account.ID, nil),
			platformerrors.ErrEmptyInputParameter,
		)

		must.ErrorIs(t,
			store.UpdateAccountBilling(t.Context(), testScope, account.ID, &BillingUpdate{
				Status: pointer.To(BillingStatus("nonsense")),
			}),
			platformerrors.ErrUnrecognizedInputValue,
		)
	})

	t.Run("refuses a write aimed at another directory", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		owner := createUser(t, store, newUser("ada"))
		account := createAccountFor(t, store, owner, "Acme")

		// The scope is in the predicate, so this matches nothing — and reporting
		// success for a write that touched no row is what would tell the caller
		// their change landed.
		err := store.UpdateAccountBilling(t.Context(), otherScope, account.ID, &BillingUpdate{
			Status: pointer.To(BillingPaid),
		})
		must.ErrorIs(t, err, ErrAccountNotFound)

		unchanged, err := store.GetAccount(t.Context(), testScope, account.ID)
		must.NoError(t, err)
		test.EqOp(t, BillingUnpaid, unchanged.BillingStatus)
	})
}
