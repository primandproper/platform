package billing

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/primandproper/platform-go/v13/billing/internal/billingdb"
	"github.com/primandproper/platform-go/v13/database"
	platformerrors "github.com/primandproper/platform-go/v13/errors"
	"github.com/primandproper/platform-go/v13/filtering"
	"github.com/primandproper/platform-go/v13/identifiers"
	"github.com/primandproper/platform-go/v13/observability"
	"github.com/primandproper/platform-go/v13/tenancy"
)

// The SQLStore's PurchaseStore: the one-time half.
var _ PurchaseStore = (*SQLStore)(nil)

// CreatePurchase records a sale in the scope, outstanding.
func (s *SQLStore) CreatePurchase(
	ctx context.Context,
	scope tenancy.Scope,
	purchase *Purchase,
) (*Purchase, error) {
	ctx, op := s.o11y.Begin(ctx, observability.WithValue(scopeKey, scope.String()))
	defer op.End()

	if purchase == nil {
		return nil, op.Error(ErrNilPurchase, "creating purchase")
	}

	if err := scope.Validate(); err != nil {
		return nil, op.Error(err, "creating purchase")
	}

	created := *purchase
	created.Scope = scope
	created.CompletedAt = utcPtr(created.CompletedAt)
	created.normalize()

	if err := created.validate(); err != nil {
		return nil, op.Error(err, "creating purchase")
	}

	if created.ID == "" {
		created.ID = identifiers.New()
	}

	op.Set(purchaseKey, created.ID)
	op.Set(accountKey, created.BelongsToAccount)

	if err := s.client.WithTransaction(ctx, func(q database.Tx) error {
		if err := s.ensurePurchaseExternalIDFree(ctx, q, scope, created.ExternalTransactionID); err != nil {
			return err
		}

		if err := s.q.CreatePurchase(ctx, q, createPurchaseParams(&created, scope)); err != nil {
			return platformerrors.Wrap(err, "creating purchase")
		}

		row, err := s.q.GetPurchaseCreatedAt(ctx, q, billingdb.GetPurchaseCreatedAtParams{ID: created.ID})
		if err != nil {
			return platformerrors.Wrap(err, "reading back the purchase's creation time")
		}

		created.CreatedAt = row.CreatedAt.UTC()

		return nil
	}); err != nil {
		return nil, op.Error(err, "creating purchase")
	}

	return &created, nil
}

// GetPurchase reads one of the scope's live purchases by id.
func (s *SQLStore) GetPurchase(ctx context.Context, scope tenancy.Scope, purchaseID string) (*Purchase, error) {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(purchaseKey, purchaseID),
	)
	defer op.End()

	if err := scope.Validate(); err != nil {
		return nil, op.Error(err, "reading purchase %q", purchaseID)
	}

	if err := requireID(purchaseID); err != nil {
		return nil, op.Error(err, "reading purchase %q", purchaseID)
	}

	row, err := s.q.GetPurchase(ctx, s.client.Reader(),
		billingdb.GetPurchaseParams{ID: purchaseID, Scope: scope})
	if err != nil {
		return nil, op.Error(notFound(err, ErrPurchaseNotFound), "reading purchase %q", purchaseID)
	}

	return purchaseFromRow(&row), nil
}

// GetPurchaseByExternalID reads one live purchase by the payment provider's
// identifier for the payment. The statement behind it sees archived rows,
// because it is also the collision check the write runs.
func (s *SQLStore) GetPurchaseByExternalID(
	ctx context.Context,
	scope tenancy.Scope,
	externalTransactionID string,
) (*Purchase, error) {
	ctx, op := s.o11y.Begin(ctx, observability.WithValue(scopeKey, scope.String()))
	defer op.End()

	if err := scope.Validate(); err != nil {
		return nil, op.Error(err, "reading purchase by external id")
	}

	purchase, err := s.readPurchaseByExternalID(ctx, s.client.Reader(), scope, externalTransactionID)
	if err != nil {
		return nil, op.Error(err, "reading purchase by external id")
	}

	if purchase.ArchivedAt != nil {
		return nil, op.Error(ErrPurchaseNotFound, "reading purchase by external id")
	}

	op.Set(purchaseKey, purchase.ID)

	return purchase, nil
}

// ListPurchases pages every purchase in the scope.
func (s *SQLStore) ListPurchases(
	ctx context.Context,
	scope tenancy.Scope,
	filter *filtering.QueryFilter,
) (*filtering.QueryFilteredResult[Purchase], error) {
	ctx, op := s.o11y.Begin(ctx, observability.WithValue(scopeKey, scope.String()))
	defer op.End()

	if err := scope.Validate(); err != nil {
		return nil, op.Error(err, "listing purchases")
	}

	filter = pageFilter(filter)

	purchaseRows, err := sortedRows(filter,
		func() ([]billingdb.ListPurchasesRow, error) {
			return s.q.ListPurchases(ctx, s.client.Reader(), listPurchasesParams(scope, filter))
		},
		func() ([]billingdb.ListPurchasesDescendingRow, error) {
			return s.q.ListPurchasesDescending(ctx, s.client.Reader(),
				billingdb.ListPurchasesDescendingParams(listPurchasesParams(scope, filter)))
		},
		func(r billingdb.ListPurchasesDescendingRow) billingdb.ListPurchasesRow {
			return billingdb.ListPurchasesRow(r)
		})
	if err != nil {
		return nil, op.Error(err, "listing purchases")
	}

	return s.drainPurchases(op, purchaseRows, filter), nil
}

// ListPurchasesForAccount pages one account's purchases.
func (s *SQLStore) ListPurchasesForAccount(
	ctx context.Context,
	scope tenancy.Scope,
	accountID string,
	filter *filtering.QueryFilter,
) (*filtering.QueryFilteredResult[Purchase], error) {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(accountKey, accountID),
	)
	defer op.End()

	if err := scope.Validate(); err != nil {
		return nil, op.Error(err, "listing purchases for account %q", accountID)
	}

	if err := requireAccount(accountID); err != nil {
		return nil, op.Error(err, "listing purchases for account")
	}

	filter = pageFilter(filter)

	purchaseRows, err := sortedRows(filter,
		func() ([]billingdb.ListPurchasesForAccountRow, error) {
			return s.q.ListPurchasesForAccount(ctx, s.client.Reader(),
				listPurchasesForAccountParams(scope, accountID, filter))
		},
		func() ([]billingdb.ListPurchasesForAccountDescendingRow, error) {
			return s.q.ListPurchasesForAccountDescending(ctx, s.client.Reader(),
				billingdb.ListPurchasesForAccountDescendingParams(
					listPurchasesForAccountParams(scope, accountID, filter)))
		},
		func(r billingdb.ListPurchasesForAccountDescendingRow) billingdb.ListPurchasesForAccountRow {
			return billingdb.ListPurchasesForAccountRow(r)
		})
	if err != nil {
		return nil, op.Error(err, "listing purchases for account %q", accountID)
	}

	shaped := make([]billingdb.ListPurchasesRow, 0, len(purchaseRows))
	for i := range purchaseRows {
		shaped = append(shaped, billingdb.ListPurchasesRow(purchaseRows[i]))
	}

	return s.drainPurchases(op, shaped, filter), nil
}

// CompletePurchase stamps the moment the money arrived.
//
// The guard is completed_at IS NULL, in the statement, so a purchase completes
// exactly once however many times the provider delivers the event. Telling a
// replay apart from a missing purchase takes one read, made only on the losing
// path.
func (s *SQLStore) CompletePurchase(
	ctx context.Context,
	scope tenancy.Scope,
	purchaseID string,
	at time.Time,
) error {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(purchaseKey, purchaseID),
	)
	defer op.End()

	if err := scope.Validate(); err != nil {
		return op.Error(err, "completing purchase %q", purchaseID)
	}

	if err := requireID(purchaseID); err != nil {
		return op.Error(err, "completing purchase %q", purchaseID)
	}

	// The zero time is the completion with no provider behind it — a comped
	// order, a migration — and falls back to the store's clock. Anything else is
	// the provider's own settlement time, which is what revenue is recognized
	// against and is frequently older than this process's idea of now.
	stamped := at.UTC()
	if at.IsZero() {
		stamped = s.clock.Now().UTC()
	}

	count, err := s.q.CompletePurchase(ctx, s.client.Writer(), billingdb.CompletePurchaseParams{
		CompletedAt: &stamped,
		ID:          purchaseID,
		Scope:       scope,
	})
	if err != nil {
		return op.Error(platformerrors.Wrap(err, "completing purchase"),
			"completing purchase %q", purchaseID)
	}

	if count == 0 {
		return op.Error(s.refuseCompletion(ctx, scope, purchaseID), "completing purchase %q", purchaseID)
	}

	return nil
}

// ArchivePurchase retires one of the scope's purchases administratively.
func (s *SQLStore) ArchivePurchase(ctx context.Context, scope tenancy.Scope, purchaseID string) error {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(purchaseKey, purchaseID),
	)
	defer op.End()

	if err := scope.Validate(); err != nil {
		return op.Error(err, "archiving purchase %q", purchaseID)
	}

	count, err := s.q.ArchivePurchase(ctx, s.client.Writer(),
		billingdb.ArchivePurchaseParams{ID: purchaseID, Scope: scope})
	if err = guardCount(count, err, ErrPurchaseNotFound, "archiving purchase"); err != nil {
		return op.Error(err, "archiving purchase %q", purchaseID)
	}

	return nil
}

// drainPurchases turns one list statement's rows into the paged result.
func (s *SQLStore) drainPurchases(
	op observability.Operation,
	rows []billingdb.ListPurchasesRow,
	filter *filtering.QueryFilter,
) *filtering.QueryFilteredResult[Purchase] {
	page := make([]pageRow[Purchase], 0, len(rows))
	for i := range rows {
		page = append(page, purchasePageRow(&rows[i]))
	}

	op.SpanOnly(countKey, len(page))

	return drainPage(page, func(p *Purchase) string { return p.ID }, filter)
}

// refuseCompletion reports why a completion touched nothing, having lost its
// guard: a purchase that is not there, or one whose money already arrived.
func (s *SQLStore) refuseCompletion(ctx context.Context, scope tenancy.Scope, purchaseID string) error {
	if _, err := s.q.GetPurchase(ctx, s.client.Reader(),
		billingdb.GetPurchaseParams{ID: purchaseID, Scope: scope}); err != nil {
		return notFound(err, ErrPurchaseNotFound)
	}

	// The row is there and live, so the guard is what lost — which for this
	// statement means the money had already arrived. The read exists to tell that
	// apart from a purchase nobody has; it is not a second guard, and it does not
	// re-examine completed_at.
	return platformerrors.Wrapf(ErrAlreadyCompleted, "purchase %q", purchaseID)
}

// readPurchaseByExternalID is the read keyed on a provider's identifier. It sees
// archived rows; its callers decide what one means.
func (s *SQLStore) readPurchaseByExternalID(
	ctx context.Context,
	q database.SQLQueryExecutor,
	scope tenancy.Scope,
	externalTransactionID string,
) (*Purchase, error) {
	if externalTransactionID == "" {
		return nil, ErrEmptyExternalID
	}

	row, err := s.q.GetPurchaseByExternalID(ctx, q, billingdb.GetPurchaseByExternalIDParams{
		Scope:                 scope,
		ExternalTransactionID: nullable(externalTransactionID),
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrPurchaseNotFound
		}

		return nil, platformerrors.Wrap(err, "reading purchase by external id")
	}

	return purchaseFromRow((*billingdb.GetPurchaseRow)(&row)), nil
}

// ensurePurchaseExternalIDFree reports whether a provider-side transaction id is
// available to a purchase in this scope.
//
// There is no exception argument, because there is no write that carries one: a
// purchase's provider id is set when the row is written and there is no statement
// able to change it. An empty identifier is always free — it is stored as NULL,
// and NULL repeats.
func (s *SQLStore) ensurePurchaseExternalIDFree(
	ctx context.Context,
	q database.SQLQueryExecutor,
	scope tenancy.Scope,
	externalTransactionID string,
) error {
	if externalTransactionID == "" {
		return nil
	}

	_, err := s.readPurchaseByExternalID(ctx, q, scope, externalTransactionID)

	switch {
	case errors.Is(err, ErrPurchaseNotFound):
		return nil
	case err != nil:
		return err
	default:
		return platformerrors.Wrapf(ErrPurchaseExists, "external transaction id %q", externalTransactionID)
	}
}
