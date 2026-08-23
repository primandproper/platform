package identity

import (
	"context"
	"testing"
	"time"

	"github.com/primandproper/platform-go/v13/database"
	platformerrors "github.com/primandproper/platform-go/v13/errors"
	"github.com/primandproper/platform-go/v13/filtering"
	"github.com/primandproper/platform-go/v13/pointer"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func runAccountSuite(t *testing.T, env *storeEnv) {
	t.Helper()

	t.Run("creates and reads back", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		owner := createUser(t, store, newUser("ada"))
		account := createAccountFor(t, store, owner, "Acme")

		read, err := store.GetAccount(t.Context(), testScope, account.ID)
		must.NoError(t, err)
		test.EqOp(t, "Acme", read.Name)
		test.EqOp(t, owner.ID, read.OwnerUserID)
		test.EqOp(t, BillingUnpaid, read.BillingStatus)
		test.True(t, read.BillingAddress.Zero())
	})

	t.Run("hides an account from another directory", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		owner := createUser(t, store, newUser("ada"))
		account := createAccountFor(t, store, owner, "Acme")

		_, err := store.GetAccount(t.Context(), otherScope, account.ID)
		must.ErrorIs(t, err, ErrAccountNotFound)
	})

	t.Run("refuses an ownerless account", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		orphan := newAccount("Acme", "")

		err := inTransaction(t, store, func(ctx context.Context, q database.SQLQueryExecutor) error {
			return store.CreateAccount(ctx, q, orphan)
		})
		must.Error(t, err)
	})

	t.Run("writes name and address but not billing or owner", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		owner := createUser(t, store, newUser("ada"))
		other := createUser(t, store, newUser("grace"))
		account := createAccountFor(t, store, owner, "Acme")

		must.NoError(t, store.UpdateAccountBilling(t.Context(), testScope, account.ID, &BillingUpdate{
			Status: pointer.To(BillingPaid),
		}))

		// A caller writing back the Account it read before the webhook landed.
		// The name lands; the stale billing status and a substituted owner do
		// not.
		account.Name = "Acme Ltd"
		account.BillingAddress = Address{Line1: "1 High St", City: "London", Country: "GB"}
		account.BillingStatus = BillingUnpaid
		account.OwnerUserID = other.ID

		must.NoError(t, store.UpdateAccount(t.Context(), account))

		read, err := store.GetAccount(t.Context(), testScope, account.ID)
		must.NoError(t, err)
		test.EqOp(t, "Acme Ltd", read.Name)
		test.EqOp(t, "1 High St", read.BillingAddress.Line1)
		test.EqOp(t, BillingPaid, read.BillingStatus)
		test.EqOp(t, owner.ID, read.OwnerUserID)
	})

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

	t.Run("pages accounts and a user's own", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		owner := createUser(t, store, newUser("ada"))
		member := createUser(t, store, newUser("brian"))

		first := createAccountFor(t, store, owner, "First")
		second := createAccountFor(t, store, owner, "Second")

		must.NoError(t, inTransaction(t, store, func(ctx context.Context, q database.SQLQueryExecutor) error {
			return store.CreateMembership(ctx, q, &Membership{
				Scope:            testScope,
				BelongsToUser:    member.ID,
				BelongsToAccount: second.ID,
				Roles:            []string{"account_member"},
			})
		}))

		all, err := store.ListAccounts(t.Context(), testScope, nil)
		must.NoError(t, err)
		must.SliceLen(t, 2, all.Data)

		mine, err := store.ListAccountsForUser(t.Context(), testScope, member.ID, nil)
		must.NoError(t, err)
		must.SliceLen(t, 1, mine.Data)
		test.EqOp(t, second.ID, mine.Data[0].ID)

		theirs, err := store.ListAccountsForUser(t.Context(), testScope, owner.ID, nil)
		must.NoError(t, err)
		must.SliceLen(t, 2, theirs.Data)

		_ = first

		page, err := store.ListAccounts(t.Context(), testScope, &filtering.QueryFilter{
			MaxResponseSize: pointer.To(uint16(1)),
		})
		must.NoError(t, err)
		must.SliceLen(t, 1, page.Data)
	})

	t.Run("ends memberships when an account is archived", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		owner := createUser(t, store, newUser("ada"))
		account := createAccountFor(t, store, owner, "Acme")
		member := registerInto(t, store, newUser("brian"), account.ID)

		must.NoError(t, store.ArchiveAccount(t.Context(), testScope, account.ID))

		// The row is still readable, as an archived user's is: an invoice and an
		// audit entry both still name it.
		read, err := store.GetAccount(t.Context(), testScope, account.ID)
		must.NoError(t, err)
		test.True(t, read.Archived())

		// It is out of the directory's page, though.
		page, err := store.ListAccounts(t.Context(), testScope, nil)
		must.NoError(t, err)
		test.SliceEmpty(t, page.Data)

		// Members left live against an archived account keep it in their
		// switcher and keep resolving permissions through it.
		_, err = store.GetMembership(t.Context(), testScope, member.ID, account.ID)
		must.ErrorIs(t, err, ErrMembershipNotFound)

		mine, err := store.ListAccountsForUser(t.Context(), testScope, member.ID, nil)
		must.NoError(t, err)
		test.SliceEmpty(t, mine.Data)

		must.ErrorIs(t, store.ArchiveAccount(t.Context(), testScope, account.ID), ErrAccountNotFound)
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

	t.Run("stamps account times in UTC", func(t *testing.T) {
		t.Parallel()

		clk := newFixedClock(baseTime)

		store, err := NewSQLStore(env.client, WithTablePrefix(env.migrate(t)), WithClock(clk))
		must.NoError(t, err)

		owner := createUser(t, store, newUser("ada"))
		account := createAccountFor(t, store, owner, "Acme")

		read, err := store.GetAccount(t.Context(), testScope, account.ID)
		must.NoError(t, err)
		test.EqOp(t, time.UTC, read.CreatedAt.Location())
		test.EqOp(t, baseTime, read.CreatedAt)
	})
}
