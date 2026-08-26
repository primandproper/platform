package identity

import (
	"context"

	"github.com/primandproper/platform-go/v13/database"
	"github.com/primandproper/platform-go/v13/identity/internal/identitydb"
)

// The SQLStore's Registrar: the three writes that make a registration, each
// through the caller's transaction so that they commit or fail together.
var _ Registrar = (*SQLStore)(nil)

// CreateUser writes a new user through the caller's transaction.
func (s *SQLStore) CreateUser(ctx context.Context, q database.Tx, user *User) error {
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

	if err := s.q.CreateUser(ctx, q, createUserParams(user)); err != nil {
		return op.Error(err, "creating identity user")
	}

	// The creation time is the database's, and it is read back so the caller's
	// copy agrees with the row rather than holding the zero time — see
	// stampCreatedAt.
	created, readErr := s.q.GetUserCreatedAt(ctx, q, identitydb.GetUserCreatedAtParams{ID: user.ID})
	if err := stampCreatedAt(&user.CreatedAt, created.CreatedAt, readErr); err != nil {
		return op.Error(err, "creating identity user")
	}

	// Written through the caller's transaction with the row, so a registration
	// that granted a default service role cannot commit the user without it.
	// That holds because the parameter is a database.Tx: the sentence used to
	// be true only of a caller who had opened one, and nothing stopped a caller
	// who had not.
	if err := s.replaceRoles(ctx, q, s.tables.userRoles, userIDColumn, user.ID, user.ServiceRoles); err != nil {
		return op.Error(err, "creating identity user")
	}

	return nil
}

// CreateAccount writes a new account through the caller's transaction.
func (s *SQLStore) CreateAccount(ctx context.Context, q database.Tx, account *Account) error {
	ctx, op := s.o11y.Begin(ctx)
	defer op.End()

	if err := requireExecutor(q); err != nil {
		return op.Error(err, "creating identity account")
	}

	if account == nil {
		return op.Error(ErrNilAccount, "creating identity account")
	}

	account.EnsureDefaults()

	if err := account.ValidateWithContext(ctx); err != nil {
		return op.Error(err, "creating identity account")
	}

	account.ID = newID(account.ID)

	op.Set(accountIDKey, account.ID).Set(scopeKey, account.Scope.String())

	if err := s.q.CreateAccount(ctx, q, createAccountParams(account)); err != nil {
		return op.Error(err, "creating identity account")
	}

	created, readErr := s.q.GetAccountCreatedAt(ctx, q, identitydb.GetAccountCreatedAtParams{ID: account.ID})
	if err := stampCreatedAt(&account.CreatedAt, created.CreatedAt, readErr); err != nil {
		return op.Error(err, "creating identity account")
	}

	return nil
}

// CreateMembership puts a user in an account through the caller's transaction.
func (s *SQLStore) CreateMembership(ctx context.Context, q database.Tx, membership *Membership) error {
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
