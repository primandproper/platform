package identity

import (
	"context"

	"github.com/primandproper/platform-go/v13/database"
	platformerrors "github.com/primandproper/platform-go/v13/errors"
	"github.com/primandproper/platform-go/v13/filtering"
	"github.com/primandproper/platform-go/v13/observability"
	"github.com/primandproper/platform-go/v13/tenancy"
)

// CreateAccount writes a new account through the caller's executor.
func (s *SQLStore) CreateAccount(ctx context.Context, q database.SQLQueryExecutor, account *Account) error {
	ctx, op := s.o11y.Begin(ctx)
	defer op.End()

	if err := requireExecutor(q); err != nil {
		return op.Error(err, "creating identity account")
	}

	if account == nil {
		return op.Error(ErrNilAccount, "creating identity account")
	}

	account.EnsureDefaults()

	if err := account.ValidateWithContext(ctx); err != nil {
		return op.Error(err, "creating identity account")
	}

	account.ID = newID(account.ID)
	account.CreatedAt = s.now()

	op.Set(accountIDKey, account.ID).Set(scopeKey, account.Scope.String())

	query, args := s.tables.buildInsertAccount(s.dialect, account, account.CreatedAt)
	if _, err := q.ExecContext(ctx, query, args...); err != nil {
		return op.Error(err, "creating identity account")
	}

	return nil
}

// GetAccount reads one of the scope's accounts, archived accounts included.
func (s *SQLStore) GetAccount(ctx context.Context, scope tenancy.Scope, accountID string) (*Account, error) {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(accountIDKey, accountID),
	)
	defer op.End()

	if err := scope.Validate(); err != nil {
		return nil, op.Error(err, "reading identity account %q", accountID)
	}

	query, args := s.tables.buildSelectAccount(s.dialect, scope, accountID)

	account, err := scanAccount(s.client.Reader().QueryRowContext(ctx, query, args...))
	if err != nil {
		return nil, op.Error(notFound(err, ErrAccountNotFound), "reading identity account %q", accountID)
	}

	return account, nil
}

// ListAccounts pages the scope's accounts.
func (s *SQLStore) ListAccounts(ctx context.Context, scope tenancy.Scope, filter *filtering.QueryFilter) (*filtering.QueryFilteredResult[Account], error) {
	ctx, op := s.o11y.Begin(ctx, observability.WithValue(scopeKey, scope.String()))
	defer op.End()

	if err := scope.Validate(); err != nil {
		return nil, op.Error(err, "listing identity accounts")
	}

	filter, cursor, limit := pageWindow(filter)

	query, args := s.tables.buildListAccounts(s.dialect, scope, cursor, limit)
	countQuery, countArgs := s.tables.buildCountAccounts(s.dialect, scope)

	return s.pageAccounts(ctx, op, filter, query, args, countQuery, countArgs)
}

// ListAccountsForUser pages the accounts a user is a live member of.
func (s *SQLStore) ListAccountsForUser(ctx context.Context, scope tenancy.Scope, userID string, filter *filtering.QueryFilter) (*filtering.QueryFilteredResult[Account], error) {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(userIDKey, userID),
	)
	defer op.End()

	if err := scope.Validate(); err != nil {
		return nil, op.Error(err, "listing identity accounts for user")
	}

	filter, cursor, limit := pageWindow(filter)

	query, args := s.tables.buildListAccountsForUser(s.dialect, scope, userID, cursor, limit)
	countQuery, countArgs := s.tables.buildCountAccountsForUser(s.dialect, scope, userID)

	return s.pageAccounts(ctx, op, filter, query, args, countQuery, countArgs)
}

// pageAccounts runs an account page and its count.
func (s *SQLStore) pageAccounts(
	ctx context.Context,
	op observability.Operation,
	filter *filtering.QueryFilter,
	query string, args []any,
	countQuery string, countArgs []any,
) (*filtering.QueryFilteredResult[Account], error) {
	accounts, err := database.ScanAll(ctx, s.client.Reader(), "identity account", query, args, scanAccount)
	if err != nil {
		return nil, op.Error(err, "listing identity accounts")
	}

	var total uint64
	if err = s.client.Reader().QueryRowContext(ctx, countQuery, countArgs...).Scan(&total); err != nil {
		return nil, op.Error(err, "counting identity accounts")
	}

	op.SpanOnly(countKey, len(accounts))

	return filtering.NewQueryFilteredResult(
		accounts, uint64(len(accounts)), total,
		func(a *Account) string { return a.ID },
		filter,
	), nil
}

// UpdateAccount writes the account's name and billing address.
func (s *SQLStore) UpdateAccount(ctx context.Context, account *Account) error {
	ctx, op := s.o11y.Begin(ctx)
	defer op.End()

	if account == nil {
		return op.Error(ErrNilAccount, "updating identity account")
	}

	if err := account.ValidateWithContext(ctx); err != nil {
		return op.Error(err, "updating identity account")
	}

	op.Set(accountIDKey, account.ID).Set(scopeKey, account.Scope.String())

	query, args := s.tables.buildUpdateAccount(s.dialect, account, s.now())

	if err := s.execExpectingRow(ctx, op, s.client.Writer(), query, args, ErrAccountNotFound, "updating identity account"); err != nil {
		return op.Error(err, "updating identity account")
	}

	return nil
}

// UpdateAccountBilling writes only the billing fields the update names.
func (s *SQLStore) UpdateAccountBilling(ctx context.Context, scope tenancy.Scope, accountID string, update *BillingUpdate) error {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(accountIDKey, accountID),
	)
	defer op.End()

	if err := scope.Validate(); err != nil {
		return op.Error(err, "updating identity account billing")
	}

	if err := update.ValidateWithContext(ctx); err != nil {
		return op.Error(err, "updating identity account billing")
	}

	query, args := s.tables.buildUpdateAccountBilling(s.dialect, scope, accountID, update, s.now())

	if err := s.execExpectingRow(ctx, op, s.client.Writer(), query, args, ErrAccountNotFound, "updating identity account billing"); err != nil {
		return op.Error(err, "updating identity account billing")
	}

	return nil
}

// ArchiveAccount soft-deletes an account and ends every membership in it.
func (s *SQLStore) ArchiveAccount(ctx context.Context, scope tenancy.Scope, accountID string) error {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(accountIDKey, accountID),
	)
	defer op.End()

	if err := scope.Validate(); err != nil {
		return op.Error(err, "archiving identity account")
	}

	now := s.now()

	if err := s.client.WithTransaction(ctx, func(q database.SQLQueryExecutor) error {
		query, args := s.tables.buildArchiveAccount(s.dialect, scope, accountID, now)
		if err := s.execExpectingRow(ctx, op, q, query, args, ErrAccountNotFound, "archiving identity account"); err != nil {
			return err
		}

		// The memberships go with it, in the same transaction. Members left live
		// against an archived account keep it in their switcher and keep
		// resolving permissions through it.
		query, args = s.tables.buildArchiveMembershipsBy(s.dialect, membershipAccountColumn, scope, accountID, now)
		if _, err := q.ExecContext(ctx, query, args...); err != nil {
			return platformerrors.Wrap(err, "archiving identity memberships")
		}

		return nil
	}); err != nil {
		return op.Error(err, "archiving identity account")
	}

	return nil
}
