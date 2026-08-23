package identity

import (
	"context"
	"slices"

	"github.com/primandproper/platform-go/v13/database"
	platformerrors "github.com/primandproper/platform-go/v13/errors"
	"github.com/primandproper/platform-go/v13/filtering"
	"github.com/primandproper/platform-go/v13/observability"
	"github.com/primandproper/platform-go/v13/tenancy"
)

// The user columns the collision checks and the sign-in reads name. They are
// constants rather than literals at the call sites because each is spelled in
// two places — a builder's predicate and the method that calls it — and a typo
// in one of them is a query that compiles, runs, and matches nothing.
const (
	usernameColumn     = "username"
	emailAddressColumn = "email_address"
	emailTokenColumn   = "email_address_verification_token"
)

// userIDColumn is what the service-role table keys on.
const userIDColumn = "user_id"

// CreateUser writes a new user through the caller's executor.
func (s *SQLStore) CreateUser(ctx context.Context, q database.SQLQueryExecutor, user *User) error {
	ctx, op := s.o11y.Begin(ctx)
	defer op.End()

	if err := requireExecutor(q); err != nil {
		return op.Error(err, "creating identity user")
	}

	if user == nil {
		return op.Error(ErrNilUser, "creating identity user")
	}

	user.EnsureDefaults()

	if err := user.ValidateWithContext(ctx); err != nil {
		return op.Error(err, "creating identity user")
	}

	user.ID = newID(user.ID)

	op.Set(userIDKey, user.ID).
		Set(scopeKey, user.Scope.String()).
		Set(usernameKey, user.Username)

	if err := s.ensureUnique(ctx, q, usernameColumn, user.Scope, user.Username, "", ErrUsernameTaken); err != nil {
		return op.Error(err, "creating identity user")
	}

	if err := s.ensureUnique(ctx, q, emailAddressColumn, user.Scope, user.EmailAddress, "", ErrEmailAddressTaken); err != nil {
		return op.Error(err, "creating identity user")
	}

	// Stamped from the store's clock and written back onto the value, so the
	// caller's copy agrees with the row rather than being whatever zero time it
	// arrived with.
	user.CreatedAt = s.now()

	query, args := s.tables.buildInsertUser(s.dialect, user, user.CreatedAt)
	if _, err := q.ExecContext(ctx, query, args...); err != nil {
		return op.Error(err, "creating identity user")
	}

	// Written through the caller's executor with the row, so a registration
	// that granted a default service role cannot commit the user without it.
	if err := s.replaceRoles(ctx, q, s.tables.userRoles, userIDColumn, user.ID, user.ServiceRoles); err != nil {
		return op.Error(err, "creating identity user")
	}

	return nil
}

// GetUser reads one of the scope's users, archived users included.
func (s *SQLStore) GetUser(ctx context.Context, scope tenancy.Scope, userID string) (*User, error) {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(userIDKey, userID),
	)
	defer op.End()

	if err := scope.Validate(); err != nil {
		return nil, op.Error(err, "reading identity user %q", userID)
	}

	query, args := s.tables.buildSelectUser(s.dialect, scope, userID)

	user, err := scanUser(s.client.Reader().QueryRowContext(ctx, query, args...))
	if err != nil {
		return nil, op.Error(notFound(err, ErrUserNotFound), "reading identity user %q", userID)
	}

	if err = s.attachServiceRoles(ctx, s.client.Reader(), []*User{user}); err != nil {
		return nil, op.Error(err, "reading identity user %q service roles", userID)
	}

	return user, nil
}

// attachServiceRoles fills in the ServiceRoles of a batch of users with one
// query, rather than one per user — which is what a directory page would
// otherwise cost.
func (s *SQLStore) attachServiceRoles(ctx context.Context, q database.SQLQueryExecutor, users []*User) error {
	ids := make([]string, 0, len(users))
	for _, user := range users {
		ids = append(ids, user.ID)
	}

	byUser, err := s.rolesFor(ctx, q, s.tables.userRoles, userIDColumn, ids)
	if err != nil {
		return err
	}

	for _, user := range users {
		user.ServiceRoles = byUser[user.ID]
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
	if err := s.client.WithTransaction(ctx, func(q database.SQLQueryExecutor) error {
		query, args := s.tables.buildSelectUser(s.dialect, scope, userID)

		if _, err := scanUser(q.QueryRowContext(ctx, query, args...)); err != nil {
			return notFound(err, ErrUserNotFound)
		}

		return s.replaceRoles(ctx, q, s.tables.userRoles, userIDColumn, userID, roles)
	}); err != nil {
		return op.Error(err, "setting identity service roles")
	}

	return nil
}

// GetUserByUsername reads a live user by the handle they sign in with.
func (s *SQLStore) GetUserByUsername(ctx context.Context, scope tenancy.Scope, username string) (*User, error) {
	return s.liveUserBy(ctx, usernameColumn, scope, username, "reading identity user by username")
}

// GetUserByEmailAddress reads a live user by their email address.
func (s *SQLStore) GetUserByEmailAddress(ctx context.Context, scope tenancy.Scope, emailAddress string) (*User, error) {
	return s.liveUserBy(ctx, emailAddressColumn, scope, emailAddress, "reading identity user by email address")
}

// GetUserByEmailVerificationToken reads the live user a verification link names.
func (s *SQLStore) GetUserByEmailVerificationToken(ctx context.Context, scope tenancy.Scope, token string) (*User, error) {
	if token == "" {
		// An empty token is what the column holds for every user with no
		// outstanding link, so the query would match an arbitrary one of them.
		// Refusing here rather than running it is the difference between a
		// rejected verification and a verified stranger.
		return nil, platformerrors.Wrap(platformerrors.ErrEmptyInputParameter, "empty email verification token")
	}

	return s.liveUserBy(ctx, emailTokenColumn, scope, token, "reading identity user by email verification token")
}

// liveUserBy is the one implementation behind the three single-user reads that
// must exclude archived users. They differ in one column and nothing else, and
// the parts that must not differ — the scope predicate and the archived clause —
// are written once here.
func (s *SQLStore) liveUserBy(ctx context.Context, column string, scope tenancy.Scope, value, description string) (*User, error) {
	ctx, op := s.o11y.Begin(ctx, observability.WithValue(scopeKey, scope.String()))
	defer op.End()

	if err := scope.Validate(); err != nil {
		return nil, op.Error(err, "%s", description)
	}

	query, args := s.tables.buildSelectLiveUserBy(s.dialect, column, scope, value)

	user, err := scanUser(s.client.Reader().QueryRowContext(ctx, query, args...))
	if err != nil {
		return nil, op.Error(notFound(err, ErrUserNotFound), "%s", description)
	}

	if err = s.attachServiceRoles(ctx, s.client.Reader(), []*User{user}); err != nil {
		return nil, op.Error(err, "%s", description)
	}

	op.SpanOnly(userIDKey, user.ID)

	return user, nil
}

// ListUsers pages the scope's directory.
func (s *SQLStore) ListUsers(ctx context.Context, scope tenancy.Scope, filter *filtering.QueryFilter) (*filtering.QueryFilteredResult[User], error) {
	ctx, op := s.o11y.Begin(ctx, observability.WithValue(scopeKey, scope.String()))
	defer op.End()

	if err := scope.Validate(); err != nil {
		return nil, op.Error(err, "listing identity users")
	}

	filter, cursor, limit := pageWindow(filter)

	query, args := s.tables.buildListUsers(s.dialect, scope, cursor, limit)
	countQuery, countArgs := s.tables.buildCountUsers(s.dialect, scope)

	return s.pageUsers(ctx, op, filter, query, args, countQuery, countArgs)
}

// SearchUsersByUsername pages the scope's users whose username begins with
// prefix.
func (s *SQLStore) SearchUsersByUsername(ctx context.Context, scope tenancy.Scope, prefix string, filter *filtering.QueryFilter) (*filtering.QueryFilteredResult[User], error) {
	ctx, op := s.o11y.Begin(ctx, observability.WithValue(scopeKey, scope.String()))
	defer op.End()

	if err := scope.Validate(); err != nil {
		return nil, op.Error(err, "searching identity users")
	}

	filter, cursor, limit := pageWindow(filter)

	query, args := s.tables.buildSearchUsers(s.dialect, scope, prefix, cursor, limit)
	countQuery, countArgs := s.tables.buildCountSearchUsers(s.dialect, scope, prefix)

	return s.pageUsers(ctx, op, filter, query, args, countQuery, countArgs)
}

// pageUsers runs a user page and its count, redacting every row.
//
// The redaction is here rather than at each call site because it is the rule
// that can be got wrong twice: a page read is where a password hash escapes in
// bulk, and the one list method that forgot to redact would look identical to
// the ones that did not.
func (s *SQLStore) pageUsers(
	ctx context.Context,
	op observability.Operation,
	filter *filtering.QueryFilter,
	query string, args []any,
	countQuery string, countArgs []any,
) (*filtering.QueryFilteredResult[User], error) {
	users, err := database.ScanAll(ctx, s.client.Reader(), "identity user", query, args, scanUser)
	if err != nil {
		return nil, op.Error(err, "listing identity users")
	}

	if err = s.attachServiceRoles(ctx, s.client.Reader(), users); err != nil {
		return nil, op.Error(err, "listing identity user service roles")
	}

	for i := range users {
		users[i] = users[i].Redacted()
	}

	var total uint64
	if err = s.client.Reader().QueryRowContext(ctx, countQuery, countArgs...).Scan(&total); err != nil {
		return nil, op.Error(err, "counting identity users")
	}

	op.SpanOnly(countKey, len(users))

	return filtering.NewQueryFilteredResult(
		users, uint64(len(users)), total,
		func(u *User) string { return u.Username },
		filter,
	), nil
}

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

// UpdateUserPassword replaces the hash, stamps the change, and releases any
// forced password change.
func (s *SQLStore) UpdateUserPassword(ctx context.Context, scope tenancy.Scope, userID, hashedPassword string) error {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(userIDKey, userID),
	)
	defer op.End()

	if err := scope.Validate(); err != nil {
		return op.Error(err, "updating identity user password")
	}

	if hashedPassword == "" {
		// An empty hash would be written and then compared against on the next
		// sign-in, by an engine with no way to know it was never set.
		return op.Error(
			platformerrors.Wrap(platformerrors.ErrEmptyInputParameter, "empty password hash"),
			"updating identity user password",
		)
	}

	query, args := s.tables.buildUpdateUserPassword(s.dialect, scope, userID, hashedPassword, s.now())

	if err := s.execExpectingRow(ctx, op, s.client.Writer(), query, args, ErrUserNotFound, "updating identity user password"); err != nil {
		return op.Error(err, "updating identity user password")
	}

	return nil
}

// SetUserRequiresPasswordChange forces or releases a password change at next
// sign-in.
func (s *SQLStore) SetUserRequiresPasswordChange(ctx context.Context, scope tenancy.Scope, userID string, requires bool) error {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(userIDKey, userID),
	)
	defer op.End()

	if err := scope.Validate(); err != nil {
		return op.Error(err, "setting identity password change requirement")
	}

	query, args := s.tables.buildSetUserFlag(s.dialect, "requires_password_change", scope, userID, requires, s.now())

	if err := s.execExpectingRow(ctx, op, s.client.Writer(), query, args, ErrUserNotFound, "setting identity password change requirement"); err != nil {
		return op.Error(err, "setting identity password change requirement")
	}

	return nil
}

// UpdateUserTwoFactorSecret stores a new TOTP secret, unverified.
func (s *SQLStore) UpdateUserTwoFactorSecret(ctx context.Context, scope tenancy.Scope, userID, secret string) error {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(userIDKey, userID),
	)
	defer op.End()

	if err := scope.Validate(); err != nil {
		return op.Error(err, "updating identity two factor secret")
	}

	if secret == "" {
		return op.Error(
			platformerrors.Wrap(platformerrors.ErrEmptyInputParameter, "empty two factor secret"),
			"updating identity two factor secret",
		)
	}

	query, args := s.tables.buildUpdateTwoFactorSecret(s.dialect, scope, userID, secret, s.now())

	if err := s.execExpectingRow(ctx, op, s.client.Writer(), query, args, ErrUserNotFound, "updating identity two factor secret"); err != nil {
		return op.Error(err, "updating identity two factor secret")
	}

	return nil
}

// MarkUserTwoFactorSecretVerified records that the user proved possession of
// their secret.
//
// A user who has already verified matches nothing, and that reports
// ErrUserNotFound rather than succeeding silently — a second verification is
// either a replayed request or a flow that lost track of its own state, and
// both are worth surfacing.
func (s *SQLStore) MarkUserTwoFactorSecretVerified(ctx context.Context, scope tenancy.Scope, userID string) error {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(userIDKey, userID),
	)
	defer op.End()

	if err := scope.Validate(); err != nil {
		return op.Error(err, "marking identity two factor secret verified")
	}

	query, args := s.tables.buildMarkTwoFactorVerified(s.dialect, scope, userID, s.now())

	if err := s.execExpectingRow(ctx, op, s.client.Writer(), query, args, ErrUserNotFound, "marking identity two factor secret verified"); err != nil {
		return op.Error(err, "marking identity two factor secret verified")
	}

	return nil
}

// SetUserEmailAddressVerificationToken stores the token a verification link will
// carry, replacing any outstanding one.
func (s *SQLStore) SetUserEmailAddressVerificationToken(ctx context.Context, scope tenancy.Scope, userID, token string) error {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(userIDKey, userID),
	)
	defer op.End()

	if err := scope.Validate(); err != nil {
		return op.Error(err, "setting identity email verification token")
	}

	if token == "" {
		// The empty string is how "no outstanding link" is stored, so writing it
		// here would be a clear dressed as an issue.
		return op.Error(
			platformerrors.Wrap(platformerrors.ErrEmptyInputParameter, "empty email verification token"),
			"setting identity email verification token",
		)
	}

	query, args := s.tables.buildSetEmailVerificationToken(s.dialect, scope, userID, token, s.now())

	if err := s.execExpectingRow(ctx, op, s.client.Writer(), query, args, ErrUserNotFound, "setting identity email verification token"); err != nil {
		return op.Error(err, "setting identity email verification token")
	}

	return nil
}

// MarkUserEmailAddressVerified stamps the address as proven and burns the token.
func (s *SQLStore) MarkUserEmailAddressVerified(ctx context.Context, scope tenancy.Scope, userID, token string) error {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(userIDKey, userID),
	)
	defer op.End()

	if err := scope.Validate(); err != nil {
		return op.Error(err, "marking identity email address verified")
	}

	if token == "" {
		return op.Error(
			platformerrors.Wrap(platformerrors.ErrEmptyInputParameter, "empty email verification token"),
			"marking identity email address verified",
		)
	}

	query, args := s.tables.buildMarkEmailVerified(s.dialect, scope, userID, token, s.now())

	if err := s.execExpectingRow(ctx, op, s.client.Writer(), query, args, ErrUserNotFound, "marking identity email address verified"); err != nil {
		return op.Error(err, "marking identity email address verified")
	}

	return nil
}

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

	query, args := s.tables.buildUpdateAccountStatus(s.dialect, scope, userID, status, explanation, s.now())

	if err := s.execExpectingRow(ctx, op, s.client.Writer(), query, args, ErrUserNotFound, "updating identity account status"); err != nil {
		return op.Error(err, "updating identity account status")
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

	if err := s.client.WithTransaction(ctx, func(q database.SQLQueryExecutor) error {
		query, args := s.tables.buildArchiveUser(s.dialect, scope, userID, now)
		if err := s.execExpectingRow(ctx, op, q, query, args, ErrUserNotFound, "archiving identity user"); err != nil {
			return err
		}

		// The memberships go in the same transaction. A user archived with live
		// memberships still appears on the rosters of the accounts they belonged
		// to, which is the state an application discovers when a deleted
		// colleague is still listed.
		query, args = s.tables.buildArchiveMembershipsBy(s.dialect, membershipUserColumn, scope, userID, now)
		if _, err := q.ExecContext(ctx, query, args...); err != nil {
			return platformerrors.Wrap(err, "archiving identity memberships")
		}

		return nil
	}); err != nil {
		return op.Error(err, "archiving identity user")
	}

	return nil
}

// EraseUser destroys the user row through the caller's executor.
func (s *SQLStore) EraseUser(ctx context.Context, q database.SQLQueryExecutor, scope tenancy.Scope, userID string) (int64, error) {
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

// pageWindow reads the cursor and limit a page query binds out of a filter,
// defaulting a nil one.
//
// The limit goes through the filtering package's own clamp rather than being
// read straight off the filter, so a caller that asked for fifty thousand rows
// gets the ceiling every other paged read in this module applies.
func pageWindow(filter *filtering.QueryFilter) (normalized *filtering.QueryFilter, cursor string, limit int) {
	if filter == nil {
		filter = filtering.DefaultQueryFilter()
	}

	limit = int(filtering.DefaultQueryFilterLimit)
	if filter.MaxResponseSize != nil {
		limit = int(filtering.ClampResponseSize(uint64(*filter.MaxResponseSize)))
	}

	if filter.Cursor != nil {
		cursor = *filter.Cursor
	}

	return filter, cursor, limit
}

// execExpectingRow runs a write that must touch a row, mapping "touched
// nothing" onto the sentinel for the entity that was not there.
//
// It exists because an UPDATE whose predicate matched nothing is a success as
// far as the driver is concerned, and every predicate here includes the scope.
// Without this, a write aimed at another directory's user returns nil — the
// caller is told their change was applied, to a row that does not exist as far
// as they are concerned.
//
// A driver that declines to report the count is treated as a hit and counted,
// because the alternative is reporting a missing row for a write that probably
// happened. The count is the only way that assumption is visible: see
// NewSQLStore.
func (s *SQLStore) execExpectingRow(
	ctx context.Context,
	op observability.Operation,
	q database.SQLQueryExecutor,
	query string,
	args []any,
	missing error,
	operation string,
) error {
	result, err := q.ExecContext(ctx, query, args...)
	if err != nil {
		return platformerrors.Wrap(err, operation)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		// Acknowledged rather than returned, and counted, because from here the
		// write is indistinguishable from one that matched a row.
		op.Acknowledge(err, "reading rows affected by %s", operation)
		s.unreportedRowsCounter.Add(ctx, 1, storeOpAttr(operation))

		return nil
	}

	if affected == 0 {
		return missing
	}

	return nil
}

// ListUsersByIDs reads a batch of the scope's users in one query.
func (s *SQLStore) ListUsersByIDs(ctx context.Context, scope tenancy.Scope, userIDs []string) ([]*User, error) {
	ctx, op := s.o11y.Begin(ctx, observability.WithValue(scopeKey, scope.String()))
	defer op.End()

	if err := scope.Validate(); err != nil {
		return nil, op.Error(err, "reading identity users by ID")
	}

	// An empty batch is an empty answer without a query. An IN () is a syntax
	// error in two of the three dialects and would match nothing in the third,
	// so the caller's empty slice would be a driver error on Postgres and a
	// working empty page on SQLite — the kind of difference that only shows up
	// in production.
	if len(userIDs) == 0 {
		return []*User{}, nil
	}

	query, args := s.tables.buildSelectUsersByIDs(s.dialect, scope, userIDs)

	users, err := database.ScanAll(ctx, s.client.Reader(), "identity user", query, args, scanUser)
	if err != nil {
		return nil, op.Error(err, "reading identity users by ID")
	}

	if err = s.attachServiceRoles(ctx, s.client.Reader(), users); err != nil {
		return nil, op.Error(err, "reading identity user service roles")
	}

	for i := range users {
		users[i] = users[i].Redacted()
	}

	op.SpanOnly(countKey, len(users))

	return users, nil
}
