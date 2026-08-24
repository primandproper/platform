package identity

import (
	"context"

	platformerrors "github.com/primandproper/platform-go/v13/errors"
	"github.com/primandproper/platform-go/v13/observability"
	"github.com/primandproper/platform-go/v13/tenancy"
)

// The SQLStore's CredentialStore: where the authentication engines' output
// lands. Every method here writes exactly one credential fact, so that none of
// them can be reached by a read-modify-write over a whole User.
var _ CredentialStore = (*SQLStore)(nil)

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
