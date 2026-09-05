package billing

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/primandproper/platform-go/v14/billing/internal/billingdb"
	"github.com/primandproper/platform-go/v14/database"
	platformerrors "github.com/primandproper/platform-go/v14/errors"
	"github.com/primandproper/platform-go/v14/filtering"
	"github.com/primandproper/platform-go/v14/identifiers"
	"github.com/primandproper/platform-go/v14/observability"
	"github.com/primandproper/platform-go/v14/tenancy"
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

	created, err := purchaseToCreate(op, scope, purchase)
	if err != nil {
		return nil, err
	}

	if err = s.client.WithTransaction(ctx, func(q database.Tx) error {
		return s.insertPurchase(ctx, q, scope, created)
	}); err != nil {
		return nil, op.Error(err, "creating purchase")
	}

	return created, nil
}

// CreatePurchaseTx is CreatePurchase inside the caller's transaction.
//
// Every check CreatePurchase makes is made here, and every statement runs on q —
// including the product check the write is gated on, so a product created
// through CreateProductTx earlier in the same transaction is one a sale can be
// recorded against. The attribution read on the losing path runs there too; see
// [Store.CreatePurchaseTx].
func (s *SQLStore) CreatePurchaseTx(
	ctx context.Context,
	q database.Tx,
	scope tenancy.Scope,
	purchase *Purchase,
) (*Purchase, error) {
	ctx, op := s.o11y.Begin(ctx, observability.WithValue(scopeKey, scope.String()))
	defer op.End()

	if q == nil {
		return nil, op.Error(ErrNilExecutor, "creating purchase")
	}

	created, err := purchaseToCreate(op, scope, purchase)
	if err != nil {
		return nil, err
	}

	if err = s.insertPurchase(ctx, q, scope, created); err != nil {
		return nil, op.Error(err, "creating purchase")
	}

	return created, nil
}

// purchaseToCreate is the checks CreatePurchase and CreatePurchaseTx share, and
// the value they write. It runs before any transaction is opened, for the reason
// productToCreate does.
func purchaseToCreate(op observability.Operation, scope tenancy.Scope, purchase *Purchase) (*Purchase, error) {
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

	return &created, nil
}

// insertPurchase is the statements the create runs, on whatever executor the
// caller is holding: the product check, the insert-ignore, the attribution of a
// loss, and the read-back of the creation time onto created.
func (s *SQLStore) insertPurchase(
	ctx context.Context,
	q database.SQLQueryExecutor,
	scope tenancy.Scope,
	created *Purchase,
) error {
	if err := s.requireProduct(ctx, q, scope, created.ProductID); err != nil {
		return err
	}

	count, err := s.q.CreatePurchase(ctx, q, createPurchaseParams(created, scope))
	if err != nil {
		return platformerrors.Wrap(err, "creating purchase")
	}

	if count == 0 {
		return s.refusePurchaseCreate(ctx, q, scope, created)
	}

	row, err := s.q.GetPurchaseCreatedAt(ctx, q, billingdb.GetPurchaseCreatedAtParams{ID: created.ID})
	if err != nil {
		return platformerrors.Wrap(err, "reading back the purchase's creation time")
	}

	created.CreatedAt = row.CreatedAt.UTC()

	return nil
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

	return s.completePurchase(ctx, op, s.client.Writer(), scope, purchaseID, at)
}

// CompletePurchaseTx is CompletePurchase inside the caller's transaction.
//
// The guard and the attribution read behind it both run on q, so a settlement
// landing in the same transaction as the sale it settles is answered by the row
// that transaction wrote rather than by a snapshot that cannot see it. See
// [Store.CompletePurchaseTx].
func (s *SQLStore) CompletePurchaseTx(
	ctx context.Context,
	q database.Tx,
	scope tenancy.Scope,
	purchaseID string,
	at time.Time,
) error {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(purchaseKey, purchaseID),
	)
	defer op.End()

	if q == nil {
		return op.Error(ErrNilExecutor, "completing purchase %q", purchaseID)
	}

	return s.completePurchase(ctx, op, q, scope, purchaseID, at)
}

// completePurchase is the shared body of CompletePurchase and
// CompletePurchaseTx, which differ in the executor they run on and in nothing
// else — the clock fallback included, so a comped order stamps the same instant
// on both paths.
func (s *SQLStore) completePurchase(
	ctx context.Context,
	op observability.Operation,
	q database.SQLQueryExecutor,
	scope tenancy.Scope,
	purchaseID string,
	at time.Time,
) error {
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

	count, err := s.q.CompletePurchase(ctx, q, billingdb.CompletePurchaseParams{
		CompletedAt: &stamped,
		ID:          purchaseID,
		Scope:       scope,
	})
	if err != nil {
		return op.Error(platformerrors.Wrap(err, "completing purchase"),
			"completing purchase %q", purchaseID)
	}

	if count == 0 {
		return op.Error(s.refuseCompletion(ctx, q, scope, purchaseID), "completing purchase %q", purchaseID)
	}

	return nil
}

// ArchivePurchase retires one of the scope's purchases administratively.
//
// It is one statement, so it runs on the writer rather than in a transaction of
// its own; ArchivePurchaseTx is the form that joins somebody else's.
func (s *SQLStore) ArchivePurchase(ctx context.Context, scope tenancy.Scope, purchaseID string) error {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(purchaseKey, purchaseID),
	)
	defer op.End()

	return s.archivePurchase(ctx, op, s.client.Writer(), scope, purchaseID)
}

// ArchivePurchaseTx is ArchivePurchase inside the caller's transaction. See
// [Store.ArchivePurchaseTx].
func (s *SQLStore) ArchivePurchaseTx(
	ctx context.Context,
	q database.Tx,
	scope tenancy.Scope,
	purchaseID string,
) error {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(purchaseKey, purchaseID),
	)
	defer op.End()

	if q == nil {
		return op.Error(ErrNilExecutor, "archiving purchase %q", purchaseID)
	}

	return s.archivePurchase(ctx, op, q, scope, purchaseID)
}

// archivePurchase is the shared body of ArchivePurchase and ArchivePurchaseTx,
// which differ in the executor they run on and in nothing else.
func (s *SQLStore) archivePurchase(
	ctx context.Context,
	op observability.Operation,
	q database.SQLQueryExecutor,
	scope tenancy.Scope,
	purchaseID string,
) error {
	if err := scope.Validate(); err != nil {
		return op.Error(err, "archiving purchase %q", purchaseID)
	}

	count, err := s.q.ArchivePurchase(ctx, q,
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
//
// It reads through the executor the write ran on, for the reason
// refuseStatusWrite does.
func (s *SQLStore) refuseCompletion(
	ctx context.Context,
	q database.SQLQueryExecutor,
	scope tenancy.Scope,
	purchaseID string,
) error {
	if _, err := s.q.GetPurchase(ctx, q,
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

// refusePurchaseCreate says which identifier the create lost to.
//
// There is no update counterpart, as there is for products and subscriptions: a
// purchase's provider id is written with the row and there is no statement able
// to change it, so the insert-ignore is the only place the question is ever
// asked. See refuseCreate.
func (s *SQLStore) refusePurchaseCreate(
	ctx context.Context,
	q database.SQLQueryExecutor,
	scope tenancy.Scope,
	created *Purchase,
) error {
	return refuseCreate(created.ExternalTransactionID, created.ID, func() error {
		_, err := s.readPurchaseByExternalID(ctx, q, scope, created.ExternalTransactionID)

		return err
	}, ErrPurchaseNotFound, ErrPurchaseExists, nil)
}
