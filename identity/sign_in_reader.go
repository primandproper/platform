package identity

import (
	"context"

	platformerrors "github.com/primandproper/platform-go/v13/errors"
	"github.com/primandproper/platform-go/v13/identity/internal/identitydb"
	"github.com/primandproper/platform-go/v13/observability"
	"github.com/primandproper/platform-go/v13/tenancy"
)

// The SQLStore's SignInReader: the two lookups a sign-in form's handles reach
// for, and the read every authenticated request afterwards makes.
var _ SignInReader = (*SQLStore)(nil)

// GetUserByUsername reads a live user by the handle they sign in with.
func (s *SQLStore) GetUserByUsername(ctx context.Context, scope tenancy.Scope, username string) (*User, error) {
	return s.liveUser(ctx, scope, "reading identity user by username", func(ctx context.Context) (*User, error) {
		row, err := s.q.GetUserByUsername(ctx, s.client.Reader(), identitydb.GetUserByUsernameParams{
			Username: username,
			Scope:    scope,
		})
		if err != nil {
			return nil, err
		}

		return userFromUsernameRow(&row), nil
	})
}

// GetUserByEmailAddress reads a live user by their email address.
func (s *SQLStore) GetUserByEmailAddress(ctx context.Context, scope tenancy.Scope, emailAddress string) (*User, error) {
	return s.liveUser(ctx, scope, "reading identity user by email address", func(ctx context.Context) (*User, error) {
		row, err := s.q.GetUserByEmailAddress(ctx, s.client.Reader(), identitydb.GetUserByEmailAddressParams{
			EmailAddress: emailAddress,
			Scope:        scope,
		})
		if err != nil {
			return nil, err
		}

		return userFromEmailAddressRow(&row), nil
	})
}

// GetPrincipal reads a user with their memberships and resolves the active
// account.
//
// Four statements on the read side, not one and not a snapshot: the user and
// their service roles, then the memberships and the roles on those. The
// interface's doc carries what that means for a caller.
func (s *SQLStore) GetPrincipal(ctx context.Context, scope tenancy.Scope, userID, activeAccountID string) (*Principal, error) {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(userIDKey, userID),
	)
	defer op.End()

	if err := scope.Validate(); err != nil {
		return nil, op.Error(err, "reading identity principal")
	}

	user, err := s.readUser(ctx, s.client.Reader(), scope, userID)
	if err != nil {
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
