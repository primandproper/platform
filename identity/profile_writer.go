package identity

import (
	"context"

	"github.com/primandproper/platform-go/v13/database"
	platformerrors "github.com/primandproper/platform-go/v13/errors"
	"github.com/primandproper/platform-go/v13/identity/internal/identitydb"
	"github.com/primandproper/platform-go/v13/observability"
	"github.com/primandproper/platform-go/v13/tenancy"
)

// The SQLStore's ProfileWriter: what a user or an account may change about
// itself. The columns it deliberately does not write — credentials, billing
// state, ownership, status — are listed on the interface.
var _ ProfileWriter = (*SQLStore)(nil)

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

	// The uniqueness checks, the read and the write share a transaction:
	// checking outside one leaves a window in which the handle is free at the
	// check and taken at the write, which surfaces as the driver's constraint
	// violation rather than as ErrUsernameTaken.
	if err := s.client.WithTransaction(ctx, func(q database.Tx) error {
		if err := s.ensureUnique(ctx, q, usernameColumn, user.Scope, user.Username, user.ID, ErrUsernameTaken); err != nil {
			return err
		}

		if err := s.ensureUnique(ctx, q, emailAddressColumn, user.Scope, user.EmailAddress, user.ID, ErrEmailAddressTaken); err != nil {
			return err
		}

		params, err := s.profileUpdateParams(ctx, q, user)
		if err != nil {
			return err
		}

		count, err := s.q.UpdateUser(ctx, q, params)

		return guardCount(count, err, ErrUserNotFound, "updating identity user")
	}); err != nil {
		return op.Error(err, "updating identity user")
	}

	return nil
}

// profileUpdateParams assembles what the profile update binds, deciding from the
// row as it stands what email_address_verified_at should hold afterwards: what
// it holds now if the address is unchanged, and nothing if it is not.
//
// Changing an address has to clear the proof that went with it, or a user could
// move to an address they have never proven and stay verified. That used to be
// a CASE inside the UPDATE, comparing the stored address against the new one in
// the same statement, and the statement querygen renders assigns columns rather
// than computing them — so the comparison moves here, into the transaction the
// update already ran in.
//
// What that costs is precise and worth stating: the CASE read the stored
// address as part of the write, where this reads it a moment before. Another
// transaction proving the address in that gap would have its stamp overwritten
// by the value read here. The only writer of that column is MarkEmailVerified,
// which requires a token this update has no reason to be racing, and the read
// is inside the same transaction as the write — so the window is narrow and the
// loss is a verification the user can repeat, rather than an unproven address
// reading as verified, which is the direction that mattered.
func (s *SQLStore) profileUpdateParams(ctx context.Context, q database.Tx, user *User) (identitydb.UpdateUserParams, error) {
	stored, err := s.readUser(ctx, q, user.Scope, user.ID)
	if err != nil {
		return identitydb.UpdateUserParams{}, err
	}

	verifiedAt := stored.EmailAddressVerifiedAt
	if stored.EmailAddress != user.EmailAddress {
		verifiedAt = nil
	}

	return updateUserParams(user, verifiedAt), nil
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

	count, err := s.q.UpdateAccount(ctx, s.client.Writer(), updateAccountParams(account))
	if err = guardCount(count, err, ErrAccountNotFound, "updating identity account"); err != nil {
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
