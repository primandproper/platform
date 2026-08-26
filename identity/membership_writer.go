package identity

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/primandproper/platform-go/v13/database"
	platformerrors "github.com/primandproper/platform-go/v13/errors"
	"github.com/primandproper/platform-go/v13/observability"
	"github.com/primandproper/platform-go/v13/tenancy"
)

// The SQLStore's MembershipWriter: who belongs to an account and what they may
// do there.
var _ MembershipWriter = (*SQLStore)(nil)

// SetMembershipRoles replaces the roles a user holds in an account.
func (s *SQLStore) SetMembershipRoles(ctx context.Context, scope tenancy.Scope, userID, accountID string, roles []string) error {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(userIDKey, userID),
		observability.WithValue(accountIDKey, accountID),
	)
	defer op.End()

	if err := scope.Validate(); err != nil {
		return op.Error(err, "setting identity membership roles")
	}

	if len(roles) == 0 {
		// A membership with no roles is a user who belongs to an account and may
		// do nothing in it, which reads at runtime as an authorization bug rather
		// than as an empty slice somebody passed. Removing somebody is
		// RemoveMembership.
		return op.Error(
			platformerrors.Wrap(platformerrors.ErrEmptyInputParameter, "membership must carry at least one role"),
			"setting identity membership roles",
		)
	}

	if err := s.client.WithTransaction(ctx, func(q database.Tx) error {
		query, args := s.tables.buildSelectMembership(s.dialect, scope, userID, accountID)

		membership, err := scanMembership(q.QueryRowContext(ctx, query, args...))
		if err != nil {
			return notFound(err, ErrMembershipNotFound)
		}

		return s.replaceRoles(ctx, q, s.tables.membershipRoles, membershipIDColumn, membership.ID, roles)
	}); err != nil {
		return op.Error(err, "setting identity membership roles")
	}

	return nil
}

// SetDefaultAccount marks one of a user's accounts as the one they land in.
func (s *SQLStore) SetDefaultAccount(ctx context.Context, scope tenancy.Scope, userID, accountID string) error {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(userIDKey, userID),
		observability.WithValue(accountIDKey, accountID),
	)
	defer op.End()

	if err := scope.Validate(); err != nil {
		return op.Error(err, "setting identity default account")
	}

	now := s.now()

	// Both writes share a transaction, so "one default per user" cannot be left
	// half-applied: clearing without setting leaves the user with none, and
	// setting without clearing leaves them with two.
	if err := s.client.WithTransaction(ctx, func(q database.Tx) error {
		query, args := s.tables.buildSetDefaultAccount(s.dialect, scope, userID, accountID, now)
		if err := s.execExpectingRow(ctx, op, q, query, args, ErrMembershipNotFound, "setting identity default account"); err != nil {
			return err
		}

		query, args = s.tables.buildClearDefaultAccount(s.dialect, scope, userID, accountID, now)
		if _, err := q.ExecContext(ctx, query, args...); err != nil {
			return platformerrors.Wrap(err, "clearing other identity default accounts")
		}

		return nil
	}); err != nil {
		return op.Error(err, "setting identity default account")
	}

	return nil
}

// TransferAccountOwnership moves an account to a new owner.
func (s *SQLStore) TransferAccountOwnership(ctx context.Context, scope tenancy.Scope, accountID, newOwnerUserID string) error {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(accountIDKey, accountID),
		observability.WithValue(userIDKey, newOwnerUserID),
	)
	defer op.End()

	if err := scope.Validate(); err != nil {
		return op.Error(err, "transferring identity account ownership")
	}

	if newOwnerUserID == "" {
		return op.Error(
			platformerrors.Wrap(platformerrors.ErrEmptyInputParameter, "no new owner named"),
			"transferring identity account ownership",
		)
	}

	now := s.now()

	if err := s.client.WithTransaction(ctx, func(q database.Tx) error {
		account, err := s.readAccount(ctx, q, scope, accountID)
		if err != nil {
			return err
		}

		if account.OwnerUserID == newOwnerUserID {
			return nil
		}

		// The new owner gets a membership if they lack one. An owner who is not
		// a member is an account whose roster does not include the person
		// responsible for it, and every roster-driven permission check then
		// refuses them. An owner who already is one keeps the roles they have.
		query, args := s.tables.buildSelectMembership(s.dialect, scope, newOwnerUserID, accountID)

		switch _, readErr := scanMembership(q.QueryRowContext(ctx, query, args...)); {
		case readErr == nil:
		case errors.Is(readErr, sql.ErrNoRows):
			if err = s.writeMembership(ctx, q, &Membership{
				ID:               newID(""),
				Scope:            scope,
				BelongsToUser:    newOwnerUserID,
				BelongsToAccount: accountID,
				Roles:            []string{},
			}); err != nil {
				return err
			}
		default:
			return readErr
		}

		query, args = s.tables.buildTransferAccountOwnership(s.dialect, scope, accountID, account.OwnerUserID, newOwnerUserID, now)

		return s.execExpectingRow(ctx, op, q, query, args, ErrAccountNotFound, "transferring identity account ownership")
	}); err != nil {
		return op.Error(err, "transferring identity account ownership")
	}

	return nil
}

// RemoveMembership ends a user's membership in an account.
func (s *SQLStore) RemoveMembership(ctx context.Context, scope tenancy.Scope, userID, accountID string) error {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(userIDKey, userID),
		observability.WithValue(accountIDKey, accountID),
	)
	defer op.End()

	if err := scope.Validate(); err != nil {
		return op.Error(err, "removing identity membership")
	}

	now := s.now()

	if err := s.client.WithTransaction(ctx, func(q database.Tx) error {
		account, err := s.readAccount(ctx, q, scope, accountID)
		if err != nil {
			return err
		}

		if account.OwnerUserID == userID {
			return platformerrors.Wrapf(ErrLastAccountOwner, "account %q", accountID)
		}

		query, args := s.tables.buildArchiveMembership(s.dialect, scope, userID, accountID, now)
		if err = s.execExpectingRow(ctx, op, q, query, args, ErrMembershipNotFound, "removing identity membership"); err != nil {
			return err
		}

		// If that was the user's default, the default moves rather than
		// vanishing — a user with memberships and nowhere to land cannot build a
		// Principal, and the failure surfaces at their next request rather than
		// at the removal that caused it.
		return s.moveDefaultAccount(ctx, q, scope, userID, accountID, now)
	}); err != nil {
		return op.Error(err, "removing identity membership")
	}

	return nil
}

// moveDefaultAccount points a user's default at another live membership, for
// when the one being removed was it. A user with no memberships left keeps none,
// which is the honest state rather than an invented one.
func (s *SQLStore) moveDefaultAccount(
	ctx context.Context,
	q database.SQLQueryExecutor,
	scope tenancy.Scope,
	userID, removedAccountID string,
	now time.Time,
) error {
	query, args := s.tables.buildSelectFallbackAccountID(s.dialect, scope, userID, removedAccountID)

	var fallbackAccountID string

	switch err := q.QueryRowContext(ctx, query, args...).Scan(&fallbackAccountID); {
	case errors.Is(err, sql.ErrNoRows):
		return nil
	case err != nil:
		return platformerrors.Wrap(err, "reading identity fallback account")
	}

	query, args = s.tables.buildSetDefaultAccount(s.dialect, scope, userID, fallbackAccountID, now)
	if _, err := q.ExecContext(ctx, query, args...); err != nil {
		return platformerrors.Wrap(err, "moving identity default account")
	}

	return nil
}
