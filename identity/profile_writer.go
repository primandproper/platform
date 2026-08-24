package identity

import (
	"context"

	"github.com/primandproper/platform-go/v13/database"
	platformerrors "github.com/primandproper/platform-go/v13/errors"
	"github.com/primandproper/platform-go/v13/observability"
	"github.com/primandproper/platform-go/v13/tenancy"
)

// The SQLStore's ProfileWriter: what a user or an account may change about
// itself. The columns it deliberately does not write — credentials, billing
// state, ownership, status — are listed on the interface.

// UpdateUser writes the user's profile and nothing else.
func (s *SQLStore) UpdateUser(ctx context.Context, user *User) error {
	ctx, op := s.o11y.Begin(ctx)
	defer op.End()

	if user == nil {
		return op.Error(ErrNilUser, "updating identity user")
	}

	if err := user.ValidateWithContext(ctx); err != nil {
		return op.Error(err, "updating identity user")
	}

	op.Set(userIDKey, user.ID).Set(scopeKey, user.Scope.String())

	// The uniqueness checks and the write share a transaction: checking outside
	// one leaves a window in which the handle is free at the check and taken at
	// the write, which surfaces as the driver's constraint violation rather than
	// as ErrUsernameTaken.
	if err := s.client.WithTransaction(ctx, func(q database.SQLQueryExecutor) error {
		if err := s.ensureUnique(ctx, q, usernameColumn, user.Scope, user.Username, user.ID, ErrUsernameTaken); err != nil {
			return err
		}

		if err := s.ensureUnique(ctx, q, emailAddressColumn, user.Scope, user.EmailAddress, user.ID, ErrEmailAddressTaken); err != nil {
			return err
		}

		query, args := s.tables.buildUpdateUser(s.dialect, user, s.now())

		return s.execExpectingRow(ctx, op, q, query, args, ErrUserNotFound, "updating identity user")
	}); err != nil {
		return op.Error(err, "updating identity user")
	}

	return nil
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

// RecordAgreement stamps the user's acceptance of one or more documents.
func (s *SQLStore) RecordAgreement(ctx context.Context, scope tenancy.Scope, userID string, agreements ...Agreement) error {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(userIDKey, userID),
	)
	defer op.End()

	if err := scope.Validate(); err != nil {
		return op.Error(err, "recording identity agreement")
	}

	if len(agreements) == 0 {
		return op.Error(
			platformerrors.Wrap(platformerrors.ErrEmptyInputParameter, "no agreements named"),
			"recording identity agreement",
		)
	}

	for _, agreement := range agreements {
		if !agreement.Valid() {
			return op.Error(
				platformerrors.Wrapf(platformerrors.ErrUnrecognizedInputValue, "agreement %q", agreement),
				"recording identity agreement",
			)
		}
	}

	query, args := s.tables.buildRecordAgreements(s.dialect, scope, userID, agreements, s.now())

	if err := s.execExpectingRow(ctx, op, s.client.Writer(), query, args, ErrUserNotFound, "recording identity agreement"); err != nil {
		return op.Error(err, "recording identity agreement")
	}

	return nil
}
