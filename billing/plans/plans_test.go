package plans

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/primandproper/platform-go/v14/billing"
	billingmock "github.com/primandproper/platform-go/v14/billing/mock"
	"github.com/primandproper/platform-go/v14/capitalism"
	"github.com/primandproper/platform-go/v14/entitlements"
	platformerrors "github.com/primandproper/platform-go/v14/errors"
	"github.com/primandproper/platform-go/v14/filtering"
	"github.com/primandproper/platform-go/v14/tenancy"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

var testScope = tenancy.Of("acme")

const testAccount = "account-1"

// subscription is one agreement in a status, on a product.
func subscription(status capitalism.SubscriptionStatus, productID string) *billing.Subscription {
	return &billing.Subscription{
		BelongsToAccount:   testAccount,
		ProductID:          productID,
		Status:             status,
		Scope:              testScope,
		CurrentPeriodStart: time.Now().Add(-time.Hour),
		CurrentPeriodEnd:   time.Now().Add(time.Hour),
	}
}

// storeReturning is a SubscriptionStore whose current-subscription read answers
// with these rows.
func storeReturning(subscriptions ...*billing.Subscription) *billingmock.SubscriptionStoreMock {
	return &billingmock.SubscriptionStoreMock{
		ListCurrentSubscriptionsFunc: func(
			_ context.Context,
			_ tenancy.Scope,
			_ string,
			filter *filtering.QueryFilter,
		) (*filtering.QueryFilteredResult[billing.Subscription], error) {
			return filtering.NewQueryFilteredResultWithoutCounts(subscriptions,
				func(s *billing.Subscription) string { return s.ID }, filter), nil
		},
	}
}

func TestNew_Refusals(T *testing.T) {
	T.Parallel()

	T.Run("refuses a nil store", func(t *testing.T) {
		t.Parallel()

		_, err := New(nil, testScope, Entitled)
		test.ErrorIs(t, err, ErrNilStore)
	})

	T.Run("refuses a nil chooser", func(t *testing.T) {
		t.Parallel()

		// There is no default, deliberately: which statuses leave an account
		// entitled is the deployment's answer, and a package that guessed would
		// be gating one customer's features on a rule nobody chose.
		_, err := New(storeReturning(), testScope, nil)
		test.ErrorIs(t, err, ErrNilChoose)
	})

	T.Run("refuses an unset scope", func(t *testing.T) {
		t.Parallel()

		_, err := New(storeReturning(), tenancy.Scope{}, Entitled)
		test.Error(t, err)
	})
}

func TestEntitled(T *testing.T) {
	T.Parallel()

	T.Run("takes an active agreement", func(t *testing.T) {
		t.Parallel()

		plan, ok := Entitled([]*billing.Subscription{
			subscription(capitalism.SubscriptionStatusActive, "pro"),
		})
		test.True(t, ok)
		test.EqOp(t, "pro", plan)
	})

	T.Run("takes a trialing agreement", func(t *testing.T) {
		t.Parallel()

		plan, ok := Entitled([]*billing.Subscription{
			subscription(capitalism.SubscriptionStatusTrialing, "pro"),
		})
		test.True(t, ok)
		test.EqOp(t, "pro", plan)
	})

	T.Run("declines the statuses deployments disagree about", func(T *testing.T) {
		T.Parallel()

		for _, status := range []capitalism.SubscriptionStatus{
			capitalism.SubscriptionStatusPastDue,
			capitalism.SubscriptionStatusPaused,
			capitalism.SubscriptionStatusUnpaid,
			capitalism.SubscriptionStatusCanceled,
			capitalism.SubscriptionStatusIncomplete,
		} {
			T.Run(string(status), func(t *testing.T) {
				t.Parallel()

				_, ok := Entitled([]*billing.Subscription{subscription(status, "pro")})
				test.False(t, ok)
			})
		}
	})

	T.Run("skips past a declined agreement to an accepted one", func(t *testing.T) {
		t.Parallel()

		plan, ok := Entitled([]*billing.Subscription{
			subscription(capitalism.SubscriptionStatusCanceled, "old"),
			subscription(capitalism.SubscriptionStatusActive, "pro"),
		})
		test.True(t, ok)
		test.EqOp(t, "pro", plan)
	})

	T.Run("declines an empty page", func(t *testing.T) {
		t.Parallel()

		_, ok := Entitled(nil)
		test.False(t, ok)
	})
}

func TestSource_PlanFor(T *testing.T) {
	T.Parallel()

	T.Run("answers with the chosen plan", func(t *testing.T) {
		t.Parallel()

		source, err := New(storeReturning(
			subscription(capitalism.SubscriptionStatusActive, "pro")), testScope, Entitled)
		must.NoError(t, err)

		plan, err := source.PlanFor(t.Context(), testAccount)
		must.NoError(t, err)
		test.EqOp(t, "pro", plan)
	})

	T.Run("reports no plan when the chooser declines", func(t *testing.T) {
		t.Parallel()

		source, err := New(storeReturning(
			subscription(capitalism.SubscriptionStatusPastDue, "pro")), testScope, Entitled)
		must.NoError(t, err)

		_, err = source.PlanFor(t.Context(), testAccount)
		test.ErrorIs(t, err, entitlements.ErrNoPlan)
	})

	T.Run("reports no plan for an account with nothing current", func(t *testing.T) {
		t.Parallel()

		source, err := New(storeReturning(), testScope, Entitled)
		must.NoError(t, err)

		_, err = source.PlanFor(t.Context(), testAccount)
		test.ErrorIs(t, err, entitlements.ErrNoPlan)
	})

	T.Run("lets a deployment write its own rule", func(t *testing.T) {
		t.Parallel()

		// The dunning reading: keep working while the processor is still
		// retrying. It is a rule this module holds no opinion about, which is
		// the whole reason Choose is an argument.
		duringDunning := func(subscriptions []*billing.Subscription) (string, bool) {
			for _, s := range subscriptions {
				if s.Status == capitalism.SubscriptionStatusPastDue {
					return s.ProductID, true
				}
			}

			return "", false
		}

		source, err := New(storeReturning(
			subscription(capitalism.SubscriptionStatusPastDue, "pro")), testScope, duringDunning)
		must.NoError(t, err)

		plan, err := source.PlanFor(t.Context(), testAccount)
		must.NoError(t, err)
		test.EqOp(t, "pro", plan)
	})

	T.Run("passes a read failure through as a failure", func(t *testing.T) {
		t.Parallel()

		boom := platformerrors.New("the database is unwell")

		store := &billingmock.SubscriptionStoreMock{
			ListCurrentSubscriptionsFunc: func(
				_ context.Context,
				_ tenancy.Scope,
				_ string,
				_ *filtering.QueryFilter,
			) (*filtering.QueryFilteredResult[billing.Subscription], error) {
				return nil, boom
			},
		}

		source, err := New(store, testScope, Entitled)
		must.NoError(t, err)

		_, err = source.PlanFor(t.Context(), testAccount)
		test.ErrorIs(t, err, boom)

		// A failure is not "no plan": entitlements answers ErrNoPlan from its
		// fallback and anything else from its error path, and collapsing the two
		// would put every account on the fallback plan during an outage.
		test.False(t, errors.Is(err, entitlements.ErrNoPlan))
	})

	T.Run("reads the scope it was built for", func(t *testing.T) {
		t.Parallel()

		var seen tenancy.Scope

		store := &billingmock.SubscriptionStoreMock{
			ListCurrentSubscriptionsFunc: func(
				_ context.Context,
				scope tenancy.Scope,
				_ string,
				filter *filtering.QueryFilter,
			) (*filtering.QueryFilteredResult[billing.Subscription], error) {
				seen = scope

				return filtering.NewQueryFilteredResultWithoutCounts[billing.Subscription](nil,
					func(s *billing.Subscription) string { return s.ID }, filter), nil
			},
		}

		source, err := New(store, testScope, Entitled)
		must.NoError(t, err)

		_, _ = source.PlanFor(t.Context(), testAccount)
		test.EqOp(t, testScope, seen)
	})
}

var _ entitlements.PlanSource = (*Source)(nil)
