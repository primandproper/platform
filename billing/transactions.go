package billing

import (
	"context"
	"database/sql"
	"errors"

	"github.com/primandproper/platform-go/v13/billing/internal/billingdb"
	"github.com/primandproper/platform-go/v13/database"
	platformerrors "github.com/primandproper/platform-go/v13/errors"
	"github.com/primandproper/platform-go/v13/filtering"
	"github.com/primandproper/platform-go/v13/identifiers"
	"github.com/primandproper/platform-go/v13/observability"
	"github.com/primandproper/platform-go/v13/tenancy"
)

// The SQLStore's TransactionStore: the ledger.
var _ TransactionStore = (*SQLStore)(nil)

// RecordTransaction writes one payment attempt to the scope's ledger.
//
// The collision check and the insert share one transaction, and the check is the
// point of this method. Payment providers redeliver; without it the second
// delivery would either insert a second row — a number somebody reconciles by
// hand — or fail against the index with an error the caller cannot tell from an
// outage, and would therefore retry forever.
func (s *SQLStore) RecordTransaction(
	ctx context.Context,
	scope tenancy.Scope,
	transaction *Transaction,
) (*Transaction, error) {
	ctx, op := s.o11y.Begin(ctx, observability.WithValue(scopeKey, scope.String()))
	defer op.End()

	if transaction == nil {
		return nil, op.Error(ErrNilTransaction, "recording transaction")
	}

	if err := scope.Validate(); err != nil {
		return nil, op.Error(err, "recording transaction")
	}

	recorded := *transaction
	recorded.Scope = scope
	recorded.normalize()

	if err := recorded.validate(); err != nil {
		return nil, op.Error(err, "recording transaction")
	}

	if recorded.ID == "" {
		recorded.ID = identifiers.New()
	}

	op.Set(transactionKey, recorded.ID)
	op.Set(accountKey, recorded.BelongsToAccount)
	op.Set(statusKey, string(recorded.Status))

	if err := s.client.WithTransaction(ctx, func(q database.Tx) error {
		if err := s.ensureTransactionExternalIDFree(ctx, q, scope, recorded.ExternalTransactionID); err != nil {
			return err
		}

		if err := s.q.CreateTransaction(ctx, q, createTransactionParams(&recorded, scope)); err != nil {
			return platformerrors.Wrap(err, "recording transaction")
		}

		row, err := s.q.GetTransactionCreatedAt(ctx, q,
			billingdb.GetTransactionCreatedAtParams{ID: recorded.ID})
		if err != nil {
			return platformerrors.Wrap(err, "reading back the transaction's creation time")
		}

		recorded.CreatedAt = row.CreatedAt.UTC()

		return nil
	}); err != nil {
		return nil, op.Error(err, "recording transaction")
	}

	s.countTransaction(ctx, recorded.Status)

	return &recorded, nil
}

// GetTransaction reads one of the scope's live ledger rows by id.
func (s *SQLStore) GetTransaction(
	ctx context.Context,
	scope tenancy.Scope,
	transactionID string,
) (*Transaction, error) {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(transactionKey, transactionID),
	)
	defer op.End()

	if err := scope.Validate(); err != nil {
		return nil, op.Error(err, "reading transaction %q", transactionID)
	}

	if err := requireID(transactionID); err != nil {
		return nil, op.Error(err, "reading transaction %q", transactionID)
	}

	row, err := s.q.GetTransaction(ctx, s.client.Reader(),
		billingdb.GetTransactionParams{ID: transactionID, Scope: scope})
	if err != nil {
		return nil, op.Error(notFound(err, ErrTransactionNotFound), "reading transaction %q", transactionID)
	}

	return transactionFromRow(&row), nil
}

// GetTransactionByExternalID reads one live ledger row by the payment provider's
// identifier for the attempt.
//
// It is how a handler tells a redelivery from a new charge before it writes, and
// the statement behind it sees archived rows because it is also the collision
// check RecordTransaction runs. This is the caller that decides an archived hit
// is not an answer.
func (s *SQLStore) GetTransactionByExternalID(
	ctx context.Context,
	scope tenancy.Scope,
	externalTransactionID string,
) (*Transaction, error) {
	ctx, op := s.o11y.Begin(ctx, observability.WithValue(scopeKey, scope.String()))
	defer op.End()

	if err := scope.Validate(); err != nil {
		return nil, op.Error(err, "reading transaction by external id")
	}

	transaction, err := s.readTransactionByExternalID(ctx, s.client.Reader(), scope, externalTransactionID)
	if err != nil {
		return nil, op.Error(err, "reading transaction by external id")
	}

	if transaction.ArchivedAt != nil {
		return nil, op.Error(ErrTransactionNotFound, "reading transaction by external id")
	}

	op.Set(transactionKey, transaction.ID)

	return transaction, nil
}

// ListTransactions pages every ledger row in the scope.
func (s *SQLStore) ListTransactions(
	ctx context.Context,
	scope tenancy.Scope,
	filter *filtering.QueryFilter,
) (*filtering.QueryFilteredResult[Transaction], error) {
	ctx, op := s.o11y.Begin(ctx, observability.WithValue(scopeKey, scope.String()))
	defer op.End()

	if err := scope.Validate(); err != nil {
		return nil, op.Error(err, "listing transactions")
	}

	filter = pageFilter(filter)

	transactionRows, err := sortedRows(filter,
		func() ([]billingdb.ListTransactionsRow, error) {
			return s.q.ListTransactions(ctx, s.client.Reader(), listTransactionsParams(scope, filter))
		},
		func() ([]billingdb.ListTransactionsDescendingRow, error) {
			return s.q.ListTransactionsDescending(ctx, s.client.Reader(),
				billingdb.ListTransactionsDescendingParams(listTransactionsParams(scope, filter)))
		},
		func(r billingdb.ListTransactionsDescendingRow) billingdb.ListTransactionsRow {
			return billingdb.ListTransactionsRow(r)
		})
	if err != nil {
		return nil, op.Error(err, "listing transactions")
	}

	return s.drainTransactions(op, transactionRows, filter), nil
}

// ListTransactionsForAccount pages one account's ledger.
func (s *SQLStore) ListTransactionsForAccount(
	ctx context.Context,
	scope tenancy.Scope,
	accountID string,
	filter *filtering.QueryFilter,
) (*filtering.QueryFilteredResult[Transaction], error) {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(accountKey, accountID),
	)
	defer op.End()

	if err := scope.Validate(); err != nil {
		return nil, op.Error(err, "listing transactions for account %q", accountID)
	}

	if err := requireAccount(accountID); err != nil {
		return nil, op.Error(err, "listing transactions for account")
	}

	filter = pageFilter(filter)

	transactionRows, err := sortedRows(filter,
		func() ([]billingdb.ListTransactionsForAccountRow, error) {
			return s.q.ListTransactionsForAccount(ctx, s.client.Reader(),
				listTransactionsForAccountParams(scope, accountID, filter))
		},
		func() ([]billingdb.ListTransactionsForAccountDescendingRow, error) {
			return s.q.ListTransactionsForAccountDescending(ctx, s.client.Reader(),
				billingdb.ListTransactionsForAccountDescendingParams(
					listTransactionsForAccountParams(scope, accountID, filter)))
		},
		func(r billingdb.ListTransactionsForAccountDescendingRow) billingdb.ListTransactionsForAccountRow {
			return billingdb.ListTransactionsForAccountRow(r)
		})
	if err != nil {
		return nil, op.Error(err, "listing transactions for account %q", accountID)
	}

	shaped := make([]billingdb.ListTransactionsRow, 0, len(transactionRows))
	for i := range transactionRows {
		shaped = append(shaped, billingdb.ListTransactionsRow(transactionRows[i]))
	}

	return s.drainTransactions(op, shaped, filter), nil
}

// SetTransactionStatus moves an attempt's outcome.
//
// It is guarded exactly as SetSubscriptionStatus is, and the counter is
// incremented only when this call is the one that moved the row — so the
// proportions the instrument reports are outcomes rather than deliveries.
func (s *SQLStore) SetTransactionStatus(
	ctx context.Context,
	scope tenancy.Scope,
	transactionID string,
	status TransactionStatus,
) error {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(transactionKey, transactionID),
		observability.WithValue(statusKey, string(status)),
	)
	defer op.End()

	if err := scope.Validate(); err != nil {
		return op.Error(err, "setting transaction %q status", transactionID)
	}

	if err := requireID(transactionID); err != nil {
		return op.Error(err, "setting transaction %q status", transactionID)
	}

	if !status.Valid() {
		return op.Error(platformerrors.Wrapf(ErrInvalidStatus, "%q", status),
			"setting transaction %q status", transactionID)
	}

	count, err := s.q.SetTransactionStatus(ctx, s.client.Writer(), billingdb.SetTransactionStatusParams{
		Status: string(status),
		ID:     transactionID,
		Scope:  scope,
	})
	if err != nil {
		return op.Error(platformerrors.Wrap(err, "setting transaction status"),
			"setting transaction %q status", transactionID)
	}

	if count == 0 {
		return op.Error(s.refuseTransactionStatusWrite(ctx, scope, transactionID),
			"setting transaction %q status", transactionID)
	}

	s.countTransaction(ctx, status)

	return nil
}

// ArchiveTransaction retires one of the scope's ledger rows administratively.
func (s *SQLStore) ArchiveTransaction(ctx context.Context, scope tenancy.Scope, transactionID string) error {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(transactionKey, transactionID),
	)
	defer op.End()

	if err := scope.Validate(); err != nil {
		return op.Error(err, "archiving transaction %q", transactionID)
	}

	count, err := s.q.ArchiveTransaction(ctx, s.client.Writer(),
		billingdb.ArchiveTransactionParams{ID: transactionID, Scope: scope})
	if err = guardCount(count, err, ErrTransactionNotFound, "archiving transaction"); err != nil {
		return op.Error(err, "archiving transaction %q", transactionID)
	}

	return nil
}

// drainTransactions turns one list statement's rows into the paged result.
func (s *SQLStore) drainTransactions(
	op observability.Operation,
	rows []billingdb.ListTransactionsRow,
	filter *filtering.QueryFilter,
) *filtering.QueryFilteredResult[Transaction] {
	page := make([]pageRow[Transaction], 0, len(rows))
	for i := range rows {
		page = append(page, transactionPageRow(&rows[i]))
	}

	op.SpanOnly(countKey, len(page))

	return drainPage(page, func(t *Transaction) string { return t.ID }, filter)
}

// refuseTransactionStatusWrite reports why a status write touched nothing: a row
// that is not there, or one already holding the status.
func (s *SQLStore) refuseTransactionStatusWrite(
	ctx context.Context,
	scope tenancy.Scope,
	transactionID string,
) error {
	if _, err := s.q.GetTransaction(ctx, s.client.Reader(),
		billingdb.GetTransactionParams{ID: transactionID, Scope: scope}); err != nil {
		return notFound(err, ErrTransactionNotFound)
	}

	return platformerrors.Wrapf(ErrStatusUnchanged, "transaction %q", transactionID)
}

// readTransactionByExternalID is the read keyed on a provider's identifier. It
// sees archived rows; its callers decide what one means.
func (s *SQLStore) readTransactionByExternalID(
	ctx context.Context,
	q database.SQLQueryExecutor,
	scope tenancy.Scope,
	externalTransactionID string,
) (*Transaction, error) {
	if externalTransactionID == "" {
		return nil, ErrEmptyExternalID
	}

	row, err := s.q.GetTransactionByExternalID(ctx, q, billingdb.GetTransactionByExternalIDParams{
		Scope:                 scope,
		ExternalTransactionID: nullable(externalTransactionID),
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrTransactionNotFound
		}

		return nil, platformerrors.Wrap(err, "reading transaction by external id")
	}

	return transactionFromRow((*billingdb.GetTransactionRow)(&row)), nil
}

// ensureTransactionExternalIDFree reports whether a provider-side transaction id
// is already recorded in this scope.
//
// There is no exception argument, for the reason the purchase check has none: a
// ledger row's provider id is written once and no statement can change it. An
// empty identifier is always free — it is stored as NULL, and NULL repeats,
// which is what lets a deployment record an adjustment no provider knows about.
func (s *SQLStore) ensureTransactionExternalIDFree(
	ctx context.Context,
	q database.SQLQueryExecutor,
	scope tenancy.Scope,
	externalTransactionID string,
) error {
	if externalTransactionID == "" {
		return nil
	}

	_, err := s.readTransactionByExternalID(ctx, q, scope, externalTransactionID)

	switch {
	case errors.Is(err, ErrTransactionNotFound):
		return nil
	case err != nil:
		return err
	default:
		return platformerrors.Wrapf(ErrTransactionExists, "external transaction id %q", externalTransactionID)
	}
}
