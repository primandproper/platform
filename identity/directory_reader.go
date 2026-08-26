package identity

import (
	"context"

	"github.com/primandproper/platform-go/v13/database"
	"github.com/primandproper/platform-go/v13/database/querygen"
	"github.com/primandproper/platform-go/v13/filtering"
	"github.com/primandproper/platform-go/v13/identity/internal/identitydb"
	"github.com/primandproper/platform-go/v13/observability"
	"github.com/primandproper/platform-go/v13/tenancy"
)

// The SQLStore's DirectoryReader: users, accounts, and the memberships between
// them, read and never written.
var _ DirectoryReader = (*SQLStore)(nil)

// GetUser reads one of the scope's live users.
func (s *SQLStore) GetUser(ctx context.Context, scope tenancy.Scope, userID string) (*User, error) {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(userIDKey, userID),
	)
	defer op.End()

	if err := scope.Validate(); err != nil {
		return nil, op.Error(err, "reading identity user %q", userID)
	}

	user, err := s.readUser(ctx, s.client.Reader(), scope, userID)
	if err != nil {
		return nil, op.Error(err, "reading identity user %q", userID)
	}

	return user, nil
}

// readUser is the read by id, through whatever executor the caller is holding.
//
// It excludes archived users, which the statement it used to run did not. That
// is querygen's single-row read rather than a decision taken here: reading one
// row by id is not a filtered list, and a caller who wants an archived user
// back wants a different query rather than a flag on this one. It is a
// consumer-visible change and it is in the release notes.
func (s *SQLStore) readUser(
	ctx context.Context,
	q database.SQLQueryExecutor,
	scope tenancy.Scope,
	userID string,
) (*User, error) {
	row, err := s.q.GetUser(ctx, q, identitydb.GetUserParams{ID: userID, Scope: scope})
	if err != nil {
		return nil, notFound(err, ErrUserNotFound)
	}

	user := userFromRow(&row)

	if err = s.attachServiceRoles(ctx, q, []*User{user}); err != nil {
		return nil, err
	}

	return user, nil
}

// ListUsers pages the scope's directory.
func (s *SQLStore) ListUsers(ctx context.Context, scope tenancy.Scope, filter *filtering.QueryFilter) (*filtering.QueryFilteredResult[User], error) {
	ctx, op := s.o11y.Begin(ctx, observability.WithValue(scopeKey, scope.String()))
	defer op.End()

	if err := scope.Validate(); err != nil {
		return nil, op.Error(err, "listing identity users")
	}

	filter = pageFilter(filter)

	listRows, err := s.q.ListUsers(ctx, s.client.Reader(), listUsersParams(scope, filter))
	if err != nil {
		return nil, op.Error(err, "listing identity users")
	}

	rows := make([]pageRow[User], 0, len(listRows))
	for i := range listRows {
		rows = append(rows, userPageRow(&listRows[i]))
	}

	if err = s.hydrateUsers(ctx, s.client.Reader(), pageValues(rows)); err != nil {
		return nil, op.Error(err, "listing identity users")
	}

	op.SpanOnly(countKey, len(rows))

	// The cursor is the id, because the statement orders by it. A cursor naming
	// a position in an order the query does not use is a page that skips rows
	// and repeats others, with nothing reporting an error.
	return filtering.Drain(rows, pageValue, pageCounts,
		func(u *User) string { return u.ID }, filter), nil
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

	if err = s.hydrateUsers(ctx, s.client.Reader(), users); err != nil {
		return nil, op.Error(err, "reading identity user service roles")
	}

	op.SpanOnly(countKey, len(users))

	return users, nil
}

// SearchUsersByUsername pages the scope's users whose username begins with
// prefix.
//
// The prefix is a literal somebody typed, and what the statement binds is
// querygen.PrefixPattern's rendering of it — the wildcards escaped, a trailing
// one appended. Without that a typed % or _ is a wildcard rather than a
// character, and the directory comes back for a prefix of "%", which reads as a
// working search returning too much rather than as a bug.
//
// The page and the count are two statements rather than one carrying the other,
// unlike the rendered lists here whose counts ride along on their rows. The
// count answers how many usernames the prefix matched, which does not move as
// the caller pages, while the page is cut by a cursor over the column it is
// ordered by. Both come from one call in identity/internal/queries — see
// querygen.Generator.PrefixSearchQueries.
func (s *SQLStore) SearchUsersByUsername(ctx context.Context, scope tenancy.Scope, prefix string, filter *filtering.QueryFilter) (*filtering.QueryFilteredResult[User], error) {
	ctx, op := s.o11y.Begin(ctx, observability.WithValue(scopeKey, scope.String()))
	defer op.End()

	if err := scope.Validate(); err != nil {
		return nil, op.Error(err, "searching identity users")
	}

	filter = pageFilter(filter)

	// Escaped once and bound twice, so the page and the number describing it
	// cannot come to search for different things.
	pattern := querygen.PrefixPattern(prefix)

	searchRows, err := s.q.SearchUsersByUsername(ctx, s.client.Reader(), searchUsersParams(scope, pattern, filter))
	if err != nil {
		return nil, op.Error(err, "searching identity users")
	}

	users := make([]*User, 0, len(searchRows))
	for i := range searchRows {
		users = append(users, userFromSearchRow(&searchRows[i]))
	}

	if err = s.hydrateUsers(ctx, s.client.Reader(), users); err != nil {
		return nil, op.Error(err, "searching identity user service roles")
	}

	count, err := s.q.CountSearchUsersByUsername(ctx, s.client.Reader(), countSearchUsersParams(scope, pattern))
	if err != nil {
		return nil, op.Error(err, "counting identity users")
	}

	op.SpanOnly(countKey, len(users))

	// The cursor is the username, because the statement orders by it. The
	// counts are their own statement rather than riding on the rows, so an
	// empty page still reports them — the ambiguity filtering.Drain exists to
	// avoid does not arise here.
	return filtering.NewQueryFilteredResult(
		users, uint64(len(users)), countOf(count.Count),
		func(u *User) string { return u.Username },
		filter,
	), nil
}

// hydrateUsers attaches a page's service roles and redacts every user in it.
//
// Both halves are here rather than at each call site because both are rules
// that can be got wrong twice, and the redaction is the one that matters: a
// page read is where a password hash escapes in bulk, and the one list method
// that forgot would look identical to the ones that did not.
//
// It redacts through the pointer rather than returning a new slice, because its
// two callers hold the users differently — one has a plain slice, the other has
// them inside the rows carrying the page's counts — and a rule that returned a
// second slice would leave the caller with the counts holding the unredacted
// copies.
func (s *SQLStore) hydrateUsers(ctx context.Context, q database.SQLQueryExecutor, users []*User) error {
	if err := s.attachServiceRoles(ctx, q, users); err != nil {
		return err
	}

	for _, user := range users {
		*user = *user.Redacted()
	}

	return nil
}

// GetAccount reads one of the scope's accounts, archived accounts included.
func (s *SQLStore) GetAccount(ctx context.Context, scope tenancy.Scope, accountID string) (*Account, error) {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(accountIDKey, accountID),
	)
	defer op.End()

	if err := scope.Validate(); err != nil {
		return nil, op.Error(err, "reading identity account %q", accountID)
	}

	account, err := s.readAccount(ctx, s.client.Reader(), scope, accountID)
	if err != nil {
		return nil, op.Error(err, "reading identity account %q", accountID)
	}

	return account, nil
}

// readAccount is the read by id, through whatever executor the caller is
// holding.
//
// Like readUser it excludes archived rows where the statement it replaced did
// not — see there.
func (s *SQLStore) readAccount(
	ctx context.Context,
	q database.SQLQueryExecutor,
	scope tenancy.Scope,
	accountID string,
) (*Account, error) {
	row, err := s.q.GetAccount(ctx, q, identitydb.GetAccountParams{ID: accountID, Scope: scope})
	if err != nil {
		return nil, notFound(err, ErrAccountNotFound)
	}

	return accountFromRow(&row), nil
}

// ListAccounts pages the scope's accounts.
func (s *SQLStore) ListAccounts(ctx context.Context, scope tenancy.Scope, filter *filtering.QueryFilter) (*filtering.QueryFilteredResult[Account], error) {
	ctx, op := s.o11y.Begin(ctx, observability.WithValue(scopeKey, scope.String()))
	defer op.End()

	if err := scope.Validate(); err != nil {
		return nil, op.Error(err, "listing identity accounts")
	}

	filter = pageFilter(filter)

	listRows, err := s.q.ListAccounts(ctx, s.client.Reader(), listAccountsParams(scope, filter))
	if err != nil {
		return nil, op.Error(err, "listing identity accounts")
	}

	rows := make([]pageRow[Account], 0, len(listRows))
	for i := range listRows {
		rows = append(rows, accountPageRow(&listRows[i]))
	}

	op.SpanOnly(countKey, len(rows))

	return filtering.Drain(rows, pageValue, pageCounts,
		func(a *Account) string { return a.ID }, filter), nil
}

// ListAccountsForUser pages the accounts a user is a live member of.
func (s *SQLStore) ListAccountsForUser(ctx context.Context, scope tenancy.Scope, userID string, filter *filtering.QueryFilter) (*filtering.QueryFilteredResult[Account], error) {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(userIDKey, userID),
	)
	defer op.End()

	if err := scope.Validate(); err != nil {
		return nil, op.Error(err, "listing identity accounts for user")
	}

	filter, cursor, limit := pageWindow(filter)

	query, args := s.tables.buildListAccountsForUser(s.dialect, scope, userID, cursor, limit)
	countQuery, countArgs := s.tables.buildCountAccountsForUser(s.dialect, scope, userID)

	return s.pageAccounts(ctx, op, filter, query, args, countQuery, countArgs)
}

// pageAccounts runs an account page and its count.
func (s *SQLStore) pageAccounts(
	ctx context.Context,
	op observability.Operation,
	filter *filtering.QueryFilter,
	query string, args []any,
	countQuery string, countArgs []any,
) (*filtering.QueryFilteredResult[Account], error) {
	accounts, err := database.ScanAll(ctx, s.client.Reader(), "identity account", query, args, scanAccount)
	if err != nil {
		return nil, op.Error(err, "listing identity accounts")
	}

	var total uint64
	if err = s.client.Reader().QueryRowContext(ctx, countQuery, countArgs...).Scan(&total); err != nil {
		return nil, op.Error(err, "counting identity accounts")
	}

	op.SpanOnly(countKey, len(accounts))

	return filtering.NewQueryFilteredResult(
		accounts, uint64(len(accounts)), total,
		func(a *Account) string { return a.ID },
		filter,
	), nil
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
