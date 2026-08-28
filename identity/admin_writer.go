package identity

import (
	"context"
	"slices"

	"github.com/primandproper/platform-go/v13/database"
	platformerrors "github.com/primandproper/platform-go/v13/errors"
	"github.com/primandproper/platform-go/v13/identity/internal/identitydb"
	"github.com/primandproper/platform-go/v13/identity/internal/queries"
	"github.com/primandproper/platform-go/v13/observability"
	"github.com/primandproper/platform-go/v13/tenancy"
)

// The SQLStore's AdminWriter: the operator's half, whose exposure through an
// ordinary request handler is a privilege escalation.
var _ AdminWriter = (*SQLStore)(nil)

// The membership columns the archive-by-side writes name, spelled where every
// other membership statement spells them: two spellings of one column is the
// drift the rest of this package exists to prevent.
const (
	membershipUserColumn    = queries.MembershipUserColumn
	membershipAccountColumn = queries.MembershipAccountColumn
)

// UpdateUserAccountStatus moves a user between statuses.
func (s *SQLStore) UpdateUserAccountStatus(ctx context.Context, scope tenancy.Scope, userID string, status AccountStatus, explanation string) error {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(userIDKey, userID),
	)
	defer op.End()

	if err := scope.Validate(); err != nil {
		return op.Error(err, "updating identity account status")
	}

	if !status.Valid() {
		return op.Error(
			platformerrors.Wrapf(platformerrors.ErrUnrecognizedInputValue, "account status %q", status),
			"updating identity account status",
		)
	}

	count, err := s.q.UpdateUserAccountStatus(ctx, s.client.Writer(), identitydb.UpdateUserAccountStatusParams{
		ID:                       userID,
		Scope:                    scope,
		AccountStatus:            status.String(),
		AccountStatusExplanation: explanation,
	})
	if err = guardCount(count, err, ErrUserNotFound, "updating identity account status"); err != nil {
		return op.Error(err, "updating identity account status")
	}

	return nil
}

// SetUserServiceRoles replaces the roles a user holds outside any account.
//
// It replaces rather than merges, for the reason SetMembershipRoles does: a
// merging setter cannot revoke, and revocation is the operation that matters
// most on the role set that grants operator access.
func (s *SQLStore) SetUserServiceRoles(ctx context.Context, scope tenancy.Scope, userID string, roles []string) error {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(userIDKey, userID),
	)
	defer op.End()

	if err := scope.Validate(); err != nil {
		return op.Error(err, "setting identity service roles")
	}

	if slices.Contains(roles, "") {
		return op.Error(
			platformerrors.Wrap(platformerrors.ErrEmptyInputParameter, "empty service role name"),
			"setting identity service roles",
		)
	}

	// The existence check and the role write share a transaction. Without the
	// check, granting a role to a user ID that does not exist in this scope
	// writes rows nothing will ever read and reports success — and the scope is
	// the part that makes "does not exist" the common case rather than a typo.
	if err := s.client.WithTransaction(ctx, func(q database.Tx) error {
		if _, err := s.readUser(ctx, q, scope, userID); err != nil {
			return err
		}

		return s.replaceRoles(ctx, q, s.tables.userRoles, userIDColumn, userID, roles)
	}); err != nil {
		return op.Error(err, "setting identity service roles")
	}

	return nil
}

// ArchiveUser soft-deletes a user and ends every membership they hold.
func (s *SQLStore) ArchiveUser(ctx context.Context, scope tenancy.Scope, userID string) error {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(userIDKey, userID),
	)
	defer op.End()

	if err := scope.Validate(); err != nil {
		return op.Error(err, "archiving identity user")
	}

	now := s.now()

	if err := s.client.WithTransaction(ctx, func(q database.Tx) error {
		count, err := s.q.ArchiveUser(ctx, q, identitydb.ArchiveUserParams{ID: userID, Scope: scope})
		if err = guardCount(count, err, ErrUserNotFound, "archiving identity user"); err != nil {
			return err
		}

		// The memberships go in the same transaction. A user archived with live
		// memberships still appears on the rosters of the accounts they belonged
		// to, which is the state an application discovers when a deleted
		// colleague is still listed.
		query, args := s.tables.buildArchiveMembershipsBy(s.dialect, membershipUserColumn, scope, userID, now)
		if _, execErr := q.ExecContext(ctx, query, args...); execErr != nil {
			return platformerrors.Wrap(execErr, "archiving identity memberships")
		}

		return nil
	}); err != nil {
		return op.Error(err, "archiving identity user")
	}

	return nil
}

// EraseUser destroys the user row through the caller's transaction.
func (s *SQLStore) EraseUser(ctx context.Context, q database.Tx, scope tenancy.Scope, userID string) (int64, error) {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(userIDKey, userID),
	)
	defer op.End()

	if err := requireExecutor(q); err != nil {
		return 0, op.Error(err, "erasing identity user")
	}

	if err := scope.Validate(); err != nil {
		return 0, op.Error(err, "erasing identity user")
	}

	query, args := s.tables.buildEraseUser(s.dialect, scope, userID)

	result, err := q.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, op.Error(err, "erasing identity user")
	}

	// A driver that declines to report the affected count is reported as zero
	// rather than as a failure. The erasure happened; what is unavailable is the
	// number, and an eraser that aborted a whole right-to-be-forgotten
	// transaction over a missing count would be worse than one reporting a
	// conservative figure.
	erased, err := result.RowsAffected()
	if err != nil {
		op.Acknowledge(err, "reading erased identity user row count")

		return 0, nil
	}

	return erased, nil
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

	if err := s.client.WithTransaction(ctx, func(q database.Tx) error {
		count, err := s.q.ArchiveAccount(ctx, q, identitydb.ArchiveAccountParams{ID: accountID, Scope: scope})
		if err = guardCount(count, err, ErrAccountNotFound, "archiving identity account"); err != nil {
			return err
		}

		// The memberships go with it, in the same transaction. Members left live
		// against an archived account keep it in their switcher and keep
		// resolving permissions through it.
		query, args := s.tables.buildArchiveMembershipsBy(s.dialect, membershipAccountColumn, scope, accountID, now)
		if _, execErr := q.ExecContext(ctx, query, args...); execErr != nil {
			return platformerrors.Wrap(execErr, "archiving identity memberships")
		}

		return nil
	}); err != nil {
		return op.Error(err, "archiving identity account")
	}

	return nil
}
