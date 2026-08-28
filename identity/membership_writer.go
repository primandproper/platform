package identity

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/primandproper/platform-go/v13/database"
	platformerrors "github.com/primandproper/platform-go/v13/errors"
	"github.com/primandproper/platform-go/v13/identity/internal/identitydb"
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

	if err := s.client.WithTransaction(ctx, func(q database.Tx) error {
		if err := s.requireRolesOrOwnership(ctx, q, scope, userID, accountID, roles); err != nil {
			return err
		}

		membership, err := s.readMembership(ctx, q, scope, userID, accountID)
		if err != nil {
			return err
		}

		return s.replaceRoles(ctx, q, s.membershipRoleWrites(), membership.ID, roles)
	}); err != nil {
		return op.Error(err, "setting identity membership roles")
	}

	return nil
}

// requireRolesOrOwnership is SetMembershipRoles's emptiness rule, which has one
// exception and needs a read to know whether it applies.
//
// A membership with no roles is ordinarily a user who belongs to an account and
// may do nothing in it — an authorization bug at runtime rather than an empty
// slice somebody passed, and removing somebody is RemoveMembership. The
// exception is the account's owner, whose standing is the ownership itself:
// they are the one member who cannot be removed and the one every check that
// resolves through owner_user_id answers for, so a role set is something they
// may hold rather than something they need.
//
// It is here because the alternative was an asymmetry with no defensible half.
// TransferAccountOwnership mints the new owner a membership carrying no roles —
// it has none to give, and giving them one would invent a role name this
// package does not define — so refusing the same state here made the setter
// unable to produce a row the transfer creates, and left the invariant true of
// whichever door a caller happened to use.
func (s *SQLStore) requireRolesOrOwnership(
	ctx context.Context,
	q database.SQLQueryExecutor,
	scope tenancy.Scope,
	userID, accountID string,
	roles []string,
) error {
	if len(roles) > 0 {
		return nil
	}

	account, err := s.readAccount(ctx, q, scope, accountID)
	if err != nil {
		return err
	}

	if account.OwnerUserID != userID {
		return platformerrors.Wrap(platformerrors.ErrEmptyInputParameter, "membership must carry at least one role")
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

	if err := s.client.WithTransaction(ctx, func(q database.Tx) error {
		account, err := s.readAccount(ctx, q, scope, accountID)
		if err != nil {
			return err
		}

		if account.OwnerUserID == newOwnerUserID {
			return nil
		}

		// owner_user_id carries no scope and no foreign key, so nothing below
		// this refuses a new owner from another directory: the membership branch
		// takes the id on faith and the SET stores it. An account owned by
		// somebody its own directory cannot read is an account whose every
		// ownership-derived permission check resolves to nobody, and the roster
		// that does display them displays a stranger. The scoped read is the
		// refusal, and it is here rather than only inside the membership write
		// because a new owner who already holds a membership would skip that
		// path entirely.
		if _, err = s.readUser(ctx, q, scope, newOwnerUserID); err != nil {
			return err
		}

		// The new owner gets a membership if they lack one. An owner who is not
		// a member is an account whose roster does not include the person
		// responsible for it, and every roster-driven permission check then
		// refuses them. An owner who already is one keeps the roles they have.
		//
		// The minted one carries no roles, and that is the state rather than a
		// gap in it: ownership is the standing, and a role invented here would
		// be a name this package does not define being written into somebody's
		// authorization. SetMembershipRoles admits the same state for the same
		// reason — see requireRolesOrOwnership — so the owner's role set is
		// reachable from both doors and refused for everybody else at both.
		switch _, readErr := s.readMembership(ctx, q, scope, newOwnerUserID, accountID); {
		case readErr == nil:
		case errors.Is(readErr, ErrMembershipNotFound):
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

		// The owner being moved away from is in the predicate as well as the new
		// one in the SET, so two concurrent transfers cannot both succeed and
		// leave the account owned by whichever committed last: the second
		// matches nothing and reports zero rows.
		count, err := s.q.TransferAccountOwnership(ctx, q, identitydb.TransferAccountOwnershipParams{
			ID:                 accountID,
			Scope:              scope,
			OwnerUserID:        newOwnerUserID,
			CurrentOwnerUserID: account.OwnerUserID,
		})

		return guardCount(count, err, ErrAccountNotFound, "transferring identity account ownership")
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
	fallback, readErr := s.q.GetMembershipFallbackAccountID(ctx, q, identitydb.GetMembershipFallbackAccountIDParams{
		Scope:            scope,
		BelongsToUser:    userID,
		BelongsToAccount: removedAccountID,
	})

	switch {
	case errors.Is(readErr, sql.ErrNoRows):
		return nil
	case readErr != nil:
		return platformerrors.Wrap(readErr, "reading identity fallback account")
	}

	query, args := s.tables.buildSetDefaultAccount(s.dialect, scope, userID, fallback.BelongsToAccount, now)
	if _, err := q.ExecContext(ctx, query, args...); err != nil {
		return platformerrors.Wrap(err, "moving identity default account")
	}

	return nil
}
