package identity

import (
	"context"
	"database/sql"
	"errors"
	"slices"

	"github.com/primandproper/platform-go/v13/database"
	platformerrors "github.com/primandproper/platform-go/v13/errors"
	"github.com/primandproper/platform-go/v13/identity/internal/identitydb"
	"github.com/primandproper/platform-go/v13/observability"
	"github.com/primandproper/platform-go/v13/tenancy"
)

// The SQLStore's AdminWriter: the operator's half, whose exposure through an
// ordinary request handler is a privilege escalation.
var _ AdminWriter = (*SQLStore)(nil)

// The membership columns the archive-by-side writes name.
const (
	membershipUserColumn    = "belongs_to_user"
	membershipAccountColumn = "belongs_to_account"
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

// ArchiveUser soft-deletes a user and ends every membership they hold, refusing
// while they still own a live account.
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
		// The last-owner guard, which RemoveMembership has always had and this
		// did not. An owner archived out from under their accounts leaves them
		// live and answering to a user every scoped read now reports as absent,
		// which is the same ownerless account RemoveMembership refuses to
		// create — reached through a different door and discovered at the next
		// permission check rather than here. Transfer or archive the account
		// first; both are one call away, and neither can be reconstructed from
		// the failure this otherwise causes.
		owned, err := s.ownedAccountID(ctx, q, scope, userID)
		if err != nil {
			return err
		}

		if owned != "" {
			return platformerrors.Wrapf(ErrLastAccountOwner, "account %q", owned)
		}

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

// ownedAccountID returns the id of one live account the user owns in this
// scope, or the empty string when they own none.
//
// It reads an id rather than asking whether one exists because the answer the
// caller needs is which account blocked: a refusal that says an account is in
// the way and cannot say which leaves the operator to find it, and the read
// costs the same either way.
func (s *SQLStore) ownedAccountID(
	ctx context.Context,
	q database.SQLQueryExecutor,
	scope tenancy.Scope,
	userID string,
) (string, error) {
	row, err := s.q.GetOwnedAccountIDForUser(ctx, q, identitydb.GetOwnedAccountIDForUserParams{
		Scope:       scope,
		OwnerUserID: userID,
	})

	switch {
	case errors.Is(err, sql.ErrNoRows):
		return "", nil
	case err != nil:
		return "", platformerrors.Wrap(err, "reading the identity accounts a user owns")
	}

	return row.ID, nil
}

// EraseUser destroys the user row through the caller's transaction.
//
// Accounts the subject owned are left where they are, and this is the one place
// in this package where an ownerless account is a state a caller can reach:
// owner_user_id keeps naming an id that no longer exists anywhere, because an
// erasure cannot be refused the way ArchiveUser refuses. A right-to-be-forgotten
// transaction spans every domain and has to commit; a store that could decline
// it would make the subject's rights conditional on an account they may not even
// administer. So the guard sits on the path that has an alternative — archiving
// is refusable, and the refusal names the account — and this path documents what
// it leaves behind instead of inventing a resolution nobody asked for. Archiving
// the owned accounts here would take an account other members are still working
// in offline because one of them exercised a right; nulling the column is not
// open to it, since the column is NOT NULL and the sentinel that would fit is a
// user id no user has.
//
// What that means for a consumer wiring this into dataprivacy: resolve the
// subject's accounts before the erasure runs — transfer the ones with other
// members, archive the ones without — the same order ArchiveUser forces on the
// soft-delete path. See identity/migrations for why owner_user_id carries no
// REFERENCES clause when every other belongs-to column in this schema does.
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
