package identity

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/primandproper/platform-go/v13/database"
	platformerrors "github.com/primandproper/platform-go/v13/errors"
	"github.com/primandproper/platform-go/v13/filtering"
	"github.com/primandproper/platform-go/v13/observability"
	"github.com/primandproper/platform-go/v13/tenancy"
)

// The membership columns the archive-by-side writes name.
const (
	membershipUserColumn    = "belongs_to_user"
	membershipAccountColumn = "belongs_to_account"
)

// membershipIDColumn is what the membership role table keys on.
const membershipIDColumn = "membership_id"

// CreateMembership puts a user in an account through the caller's executor.
func (s *SQLStore) CreateMembership(ctx context.Context, q database.SQLQueryExecutor, membership *Membership) error {
	ctx, op := s.o11y.Begin(ctx)
	defer op.End()

	if err := requireExecutor(q); err != nil {
		return op.Error(err, "creating identity membership")
	}

	if membership == nil {
		return op.Error(ErrNilMembership, "creating identity membership")
	}

	if err := membership.ValidateWithContext(ctx); err != nil {
		return op.Error(err, "creating identity membership")
	}

	membership.ID = newID(membership.ID)
	membership.CreatedAt = s.now()

	op.Set(userIDKey, membership.BelongsToUser).
		Set(accountIDKey, membership.BelongsToAccount).
		Set(scopeKey, membership.Scope.String())

	// A user's first live membership is their default whatever the value says.
	// A user with memberships and no default has nowhere to land, and it is a
	// state that is easy to write and confusing to debug — GetPrincipal reports
	// ErrNoDefaultAccount and the caller has no obvious way to have caused it.
	existing, err := s.liveMembershipCount(ctx, q, membership.Scope, membership.BelongsToUser)
	if err != nil {
		return op.Error(err, "creating identity membership")
	}

	if existing == 0 {
		membership.DefaultAccount = true
	}

	if err = s.writeMembership(ctx, q, membership); err != nil {
		return op.Error(err, "creating identity membership")
	}

	return nil
}

// writeMembership upserts the membership row, resolves the ID the row actually
// carries, and replaces its roles.
//
// The ID is read back rather than assumed because the upsert may have taken its
// conflict branch — a user rejoining an account revives the archived membership,
// which keeps the ID it was created with. Writing the roles against the ID the
// caller generated would attach them to a membership that does not exist.
func (s *SQLStore) writeMembership(ctx context.Context, q database.SQLQueryExecutor, membership *Membership) error {
	query, args := s.tables.buildUpsertMembership(s.dialect, membership, membership.CreatedAt)
	if _, err := q.ExecContext(ctx, query, args...); err != nil {
		return platformerrors.Wrap(err, "writing identity membership")
	}

	query, args = s.tables.buildSelectMembershipID(s.dialect, membership.BelongsToUser, membership.BelongsToAccount)
	if err := q.QueryRowContext(ctx, query, args...).Scan(&membership.ID); err != nil {
		return platformerrors.Wrap(err, "reading back identity membership")
	}

	if err := s.replaceRoles(ctx, q, s.tables.membershipRoles, membershipIDColumn, membership.ID, membership.Roles); err != nil {
		return err
	}

	if !membership.DefaultAccount {
		return nil
	}

	query, args = s.tables.buildClearDefaultAccount(
		s.dialect, membership.Scope, membership.BelongsToUser, membership.BelongsToAccount, membership.CreatedAt,
	)
	if _, err := q.ExecContext(ctx, query, args...); err != nil {
		return platformerrors.Wrap(err, "clearing other identity default accounts")
	}

	return nil
}

// liveMembershipCount counts a user's live memberships, for deciding whether the
// one being written is their first.
func (s *SQLStore) liveMembershipCount(ctx context.Context, q database.SQLQueryExecutor, scope tenancy.Scope, userID string) (int, error) {
	query, args := s.tables.buildCountLiveMembershipsForUser(s.dialect, scope, userID)

	var count int
	if err := q.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		return 0, platformerrors.Wrap(err, "counting identity memberships")
	}

	return count, nil
}

// GetMembership reads the live membership between a user and an account.
func (s *SQLStore) GetMembership(ctx context.Context, scope tenancy.Scope, userID, accountID string) (*Membership, error) {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(userIDKey, userID),
		observability.WithValue(accountIDKey, accountID),
	)
	defer op.End()

	if err := scope.Validate(); err != nil {
		return nil, op.Error(err, "reading identity membership")
	}

	query, args := s.tables.buildSelectMembership(s.dialect, scope, userID, accountID)

	membership, err := scanMembership(s.client.Reader().QueryRowContext(ctx, query, args...))
	if err != nil {
		return nil, op.Error(notFound(err, ErrMembershipNotFound), "reading identity membership")
	}

	if err = s.attachMembershipRoles(ctx, s.client.Reader(), []*Membership{membership}); err != nil {
		return nil, op.Error(err, "reading identity membership roles")
	}

	return membership, nil
}

// ListMembershipsForUser returns every live membership a user holds, default
// account first.
func (s *SQLStore) ListMembershipsForUser(ctx context.Context, scope tenancy.Scope, userID string) ([]*Membership, error) {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(userIDKey, userID),
	)
	defer op.End()

	if err := scope.Validate(); err != nil {
		return nil, op.Error(err, "listing identity memberships")
	}

	memberships, err := s.readMembershipsForUser(ctx, s.client.Reader(), scope, userID)
	if err != nil {
		return nil, op.Error(err, "listing identity memberships")
	}

	op.SpanOnly(countKey, len(memberships))

	return memberships, nil
}

// readMembershipsForUser is the read behind both ListMembershipsForUser and
// GetPrincipal, roles attached.
func (s *SQLStore) readMembershipsForUser(
	ctx context.Context,
	q database.SQLQueryExecutor,
	scope tenancy.Scope,
	userID string,
) ([]*Membership, error) {
	query, args := s.tables.buildListMembershipsForUser(s.dialect, scope, userID)

	memberships, err := database.ScanAll(ctx, q, "identity membership", query, args, scanMembership)
	if err != nil {
		return nil, err
	}

	if err = s.attachMembershipRoles(ctx, q, memberships); err != nil {
		return nil, err
	}

	return memberships, nil
}

// attachMembershipRoles fills in the Roles of a batch of memberships with one
// query, rather than one per membership.
func (s *SQLStore) attachMembershipRoles(ctx context.Context, q database.SQLQueryExecutor, memberships []*Membership) error {
	ids := make([]string, 0, len(memberships))
	for _, membership := range memberships {
		ids = append(ids, membership.ID)
	}

	byMembership, err := s.rolesFor(ctx, q, s.tables.membershipRoles, membershipIDColumn, ids)
	if err != nil {
		return err
	}

	for _, membership := range memberships {
		membership.Roles = byMembership[membership.ID]
	}

	return nil
}

// GetPrincipal reads a user with their memberships and resolves the active
// account, in one round trip.
func (s *SQLStore) GetPrincipal(ctx context.Context, scope tenancy.Scope, userID, activeAccountID string) (*Principal, error) {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(userIDKey, userID),
	)
	defer op.End()

	if err := scope.Validate(); err != nil {
		return nil, op.Error(err, "reading identity principal")
	}

	query, args := s.tables.buildSelectUser(s.dialect, scope, userID)

	user, err := scanUser(s.client.Reader().QueryRowContext(ctx, query, args...))
	if err != nil {
		return nil, op.Error(notFound(err, ErrUserNotFound), "reading identity principal")
	}

	if err = s.attachServiceRoles(ctx, s.client.Reader(), []*User{user}); err != nil {
		return nil, op.Error(err, "reading identity principal")
	}

	memberships, err := s.readMembershipsForUser(ctx, s.client.Reader(), scope, userID)
	if err != nil {
		return nil, op.Error(err, "reading identity principal")
	}

	principal := &Principal{User: user.Redacted(), Memberships: memberships}

	if principal.ActiveAccountID, err = resolveActiveAccount(memberships, activeAccountID); err != nil {
		return nil, op.Error(err, "reading identity principal")
	}

	op.SpanOnly(accountIDKey, principal.ActiveAccountID)

	return principal, nil
}

// resolveActiveAccount picks the account a request is against.
//
// An empty request is answered with the user's default. A named one must appear
// among the live memberships — this is the check every hand-built session
// context eventually forgets, and forgetting it is what serves one account's
// data to another account's member, because everything downstream then trusts
// the ID it was handed.
func resolveActiveAccount(memberships []*Membership, requested string) (string, error) {
	if requested == "" {
		// The read orders default_account first, so the head is the default when
		// there is one.
		if len(memberships) > 0 && memberships[0].DefaultAccount {
			return memberships[0].BelongsToAccount, nil
		}

		return "", ErrNoDefaultAccount
	}

	for _, membership := range memberships {
		if membership.BelongsToAccount == requested {
			return requested, nil
		}
	}

	return "", platformerrors.Wrapf(ErrMembershipNotFound, "account %q", requested)
}

// ListAccountMembers pages an account's roster.
func (s *SQLStore) ListAccountMembers(ctx context.Context, scope tenancy.Scope, accountID string, filter *filtering.QueryFilter) (*filtering.QueryFilteredResult[MembershipWithUser], error) {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(accountIDKey, accountID),
	)
	defer op.End()

	if err := scope.Validate(); err != nil {
		return nil, op.Error(err, "listing identity account members")
	}

	filter, cursor, limit := pageWindow(filter)

	query, args := s.tables.buildListAccountMembers(s.dialect, scope, accountID, cursor, limit)

	members, err := database.ScanAll(ctx, s.client.Reader(), "identity account member", query, args, scanMembershipWithUser)
	if err != nil {
		return nil, op.Error(err, "listing identity account members")
	}

	memberships := make([]*Membership, 0, len(members))
	for _, member := range members {
		memberships = append(memberships, &member.Membership)
	}

	if err = s.attachMembershipRoles(ctx, s.client.Reader(), memberships); err != nil {
		return nil, op.Error(err, "listing identity account member roles")
	}

	countQuery, countArgs := s.tables.buildCountAccountMembers(s.dialect, scope, accountID)

	var total uint64
	if err = s.client.Reader().QueryRowContext(ctx, countQuery, countArgs...).Scan(&total); err != nil {
		return nil, op.Error(err, "counting identity account members")
	}

	op.SpanOnly(countKey, len(members))

	return filtering.NewQueryFilteredResult(
		members, uint64(len(members)), total,
		func(m *MembershipWithUser) string { return m.ID },
		filter,
	), nil
}

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

	if err := s.client.WithTransaction(ctx, func(q database.SQLQueryExecutor) error {
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
	if err := s.client.WithTransaction(ctx, func(q database.SQLQueryExecutor) error {
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

	if err := s.client.WithTransaction(ctx, func(q database.SQLQueryExecutor) error {
		query, args := s.tables.buildSelectAccount(s.dialect, scope, accountID)

		account, err := scanAccount(q.QueryRowContext(ctx, query, args...))
		if err != nil {
			return notFound(err, ErrAccountNotFound)
		}

		if account.OwnerUserID == newOwnerUserID {
			return nil
		}

		// The new owner gets a membership if they lack one. An owner who is not
		// a member is an account whose roster does not include the person
		// responsible for it, and every roster-driven permission check then
		// refuses them. An owner who already is one keeps the roles they have.
		query, args = s.tables.buildSelectMembership(s.dialect, scope, newOwnerUserID, accountID)

		switch _, readErr := scanMembership(q.QueryRowContext(ctx, query, args...)); {
		case readErr == nil:
		case errors.Is(readErr, sql.ErrNoRows):
			if err = s.writeMembership(ctx, q, &Membership{
				ID:               newID(""),
				Scope:            scope,
				BelongsToUser:    newOwnerUserID,
				BelongsToAccount: accountID,
				Roles:            []string{},
				CreatedAt:        now,
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

	if err := s.client.WithTransaction(ctx, func(q database.SQLQueryExecutor) error {
		query, args := s.tables.buildSelectAccount(s.dialect, scope, accountID)

		account, err := scanAccount(q.QueryRowContext(ctx, query, args...))
		if err != nil {
			return notFound(err, ErrAccountNotFound)
		}

		if account.OwnerUserID == userID {
			return platformerrors.Wrapf(ErrLastAccountOwner, "account %q", accountID)
		}

		query, args = s.tables.buildArchiveMembership(s.dialect, scope, userID, accountID, now)
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
