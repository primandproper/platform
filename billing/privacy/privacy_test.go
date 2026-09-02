package privacy

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/primandproper/platform-go/v14/billing"
	billingmock "github.com/primandproper/platform-go/v14/billing/mock"
	"github.com/primandproper/platform-go/v14/dataprivacy"
	platformerrors "github.com/primandproper/platform-go/v14/errors"
	"github.com/primandproper/platform-go/v14/filtering"
	"github.com/primandproper/platform-go/v14/tenancy"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

var testScope = tenancy.Of("acme")

const testAccount = "account-1"

var testSubject = dataprivacy.Subject{ID: testAccount}

// pageOf answers one read with these rows and then with nothing, which is what
// makes the collector's cursor walk terminate.
func pageOf[T any](rows []*T, id func(*T) string) func(
	context.Context, tenancy.Scope, string, *filtering.QueryFilter,
) (*filtering.QueryFilteredResult[T], error) {
	served := false

	return func(
		_ context.Context,
		_ tenancy.Scope,
		_ string,
		filter *filtering.QueryFilter,
	) (*filtering.QueryFilteredResult[T], error) {
		if served {
			return filtering.NewQueryFilteredResultWithoutCounts[T](nil, id, filter), nil
		}

		served = true

		return filtering.NewQueryFilteredResultWithoutCounts(rows, id, filter), nil
	}
}

// fullStore answers all three account-keyed reads with one row each.
func fullStore() *billingmock.StoreMock {
	return &billingmock.StoreMock{
		ListSubscriptionsForAccountFunc: pageOf(
			[]*billing.Subscription{{ID: "sub-1", BelongsToAccount: testAccount, ProductID: "pro"}},
			func(s *billing.Subscription) string { return s.ID }),
		ListPurchasesForAccountFunc: pageOf(
			[]*billing.Purchase{{ID: "pur-1", BelongsToAccount: testAccount, AmountCents: 999}},
			func(p *billing.Purchase) string { return p.ID }),
		ListTransactionsForAccountFunc: pageOf(
			[]*billing.Transaction{{ID: "txn-1", BelongsToAccount: testAccount, AmountCents: 999}},
			func(t *billing.Transaction) string { return t.ID }),
	}
}

func TestNewCollector_Refusals(T *testing.T) {
	T.Parallel()

	T.Run("refuses a nil store", func(t *testing.T) {
		t.Parallel()

		_, err := NewCollector(nil, FixedAccounts(testScope))
		test.ErrorIs(t, err, ErrNilStore)
	})

	T.Run("refuses a nil resolver", func(t *testing.T) {
		t.Parallel()

		// There is no default: a resolver that answered "the account whose id
		// equals the subject id" would export nothing for most deployments and
		// report success doing it.
		_, err := NewCollector(fullStore(), nil)
		test.ErrorIs(t, err, ErrNilAccountResolver)
	})
}

func TestCollector_Collect(T *testing.T) {
	T.Parallel()

	T.Run("exports all three tables for the resolved account", func(t *testing.T) {
		t.Parallel()

		collector, err := NewCollector(fullStore(), FixedAccounts(testScope))
		must.NoError(t, err)

		body, err := collector.Collect(t.Context(), testSubject)
		must.NoError(t, err)

		var exports []Export
		must.NoError(t, json.Unmarshal(body, &exports))
		must.SliceLen(t, 1, exports)

		test.EqOp(t, testAccount, exports[0].Account)
		test.EqOp(t, testScope.String(), exports[0].Scope)
		test.SliceLen(t, 1, exports[0].Subscriptions)
		test.SliceLen(t, 1, exports[0].Purchases)
		test.SliceLen(t, 1, exports[0].Transactions)
	})

	T.Run("asks for archived rows", func(t *testing.T) {
		t.Parallel()

		var sawArchived bool

		store := fullStore()
		store.ListSubscriptionsForAccountFunc = func(
			_ context.Context,
			_ tenancy.Scope,
			_ string,
			filter *filtering.QueryFilter,
		) (*filtering.QueryFilteredResult[billing.Subscription], error) {
			sawArchived = filter.IncludeArchived != nil && *filter.IncludeArchived

			return filtering.NewQueryFilteredResultWithoutCounts[billing.Subscription](nil,
				func(s *billing.Subscription) string { return s.ID }, filter), nil
		}

		collector, err := NewCollector(store, FixedAccounts(testScope))
		must.NoError(t, err)

		_, err = collector.Collect(t.Context(), testSubject)
		must.NoError(t, err)

		// An archived subscription is still something the subject was sold, and
		// an export showing only what an operator had not yet tidied away would
		// answer a different question than the right of access asks.
		test.True(t, sawArchived)
	})

	T.Run("exports every account the resolver names", func(t *testing.T) {
		t.Parallel()

		resolve := func(context.Context, dataprivacy.Subject) ([]Account, error) {
			return []Account{
				{Scope: testScope, ID: "account-1"},
				{Scope: tenancy.Of("other"), ID: "account-2"},
			}, nil
		}

		collector, err := NewCollector(fullStore(), resolve)
		must.NoError(t, err)

		body, err := collector.Collect(t.Context(), testSubject)
		must.NoError(t, err)

		var exports []Export
		must.NoError(t, json.Unmarshal(body, &exports))
		test.SliceLen(t, 2, exports)
	})

	T.Run("exports nothing for a subject who has bought nothing", func(t *testing.T) {
		t.Parallel()

		resolve := func(context.Context, dataprivacy.Subject) ([]Account, error) {
			return nil, nil
		}

		collector, err := NewCollector(fullStore(), resolve)
		must.NoError(t, err)

		body, err := collector.Collect(t.Context(), testSubject)
		must.NoError(t, err)

		var exports []Export
		must.NoError(t, json.Unmarshal(body, &exports))
		test.SliceEmpty(t, exports)
	})

	T.Run("passes a resolver failure through", func(t *testing.T) {
		t.Parallel()

		boom := platformerrors.New("no directory")

		collector, err := NewCollector(fullStore(),
			func(context.Context, dataprivacy.Subject) ([]Account, error) { return nil, boom })
		must.NoError(t, err)

		_, err = collector.Collect(t.Context(), testSubject)
		test.ErrorIs(t, err, boom)
	})

	T.Run("passes a read failure through", func(t *testing.T) {
		t.Parallel()

		boom := platformerrors.New("the database is unwell")

		store := fullStore()
		store.ListPurchasesForAccountFunc = func(
			context.Context, tenancy.Scope, string, *filtering.QueryFilter,
		) (*filtering.QueryFilteredResult[billing.Purchase], error) {
			return nil, boom
		}

		collector, err := NewCollector(store, FixedAccounts(testScope))
		must.NoError(t, err)

		_, err = collector.Collect(t.Context(), testSubject)
		test.ErrorIs(t, err, boom)
	})
}

// TestPackage_ShipsNoEraser is the ruling, checked rather than only written down.
//
// Financial records carry a statutory retention that outranks a right to
// erasure, so the only correct Eraser here is one that erases nothing — and
// shipping it would invite a deployment to register it and believe its billing
// history had been deleted. If somebody adds one, this stops compiling and they
// have to read the package documentation first.
func TestPackage_ShipsNoEraser(t *testing.T) {
	t.Parallel()

	var collector any = &Collector{}

	_, isEraser := collector.(dataprivacy.Eraser)
	test.False(t, isEraser)

	_, isCollector := collector.(dataprivacy.Collector)
	test.True(t, isCollector)
}
