package identity

import (
	"github.com/primandproper/platform-go/v13/database/dialect"
	"github.com/primandproper/platform-go/v13/database/querygen"
	platformerrors "github.com/primandproper/platform-go/v13/errors"
	"github.com/primandproper/platform-go/v13/identity/internal/queries"
)

// The conventional half of this package's SQL, rendered once at construction by
// database/querygen rather than composed per call.
//
// Thirteen statements against three tables, and none of them is written here.
// Each is a call into the same statement function `make generate` renders the
// canonical .sql from, with the argument references rewritten into this
// dialect's bind markers and the consumer's prefix on the table name — so what
// executes is what sqlc checked, differing by one substitution. See
// identity/internal/queries for the column lists both halves read.
//
// What this buys is specific, and each part of it used to be a comment in this
// package admitting the opposite. The projection is a list, not a string, and
// the same list the scan targets are written from. The placeholders are
// numbered by the renderer rather than by hand, so the mistake the binder type
// exists to prevent is unspellable in these thirteen. And the filtered reads
// carry the created/updated windows, the archived toggle and both counts,
// which the hand-written pages did not have and could not have acquired
// without a fourth copy of the window predicates.

// scopeMatch is the tenancy predicate every rendered statement carries.
//
// It is a Match rather than a value because Match names a column and querygen
// counts its occurrences where they land — a list query renders it three times,
// once in the SELECT and once in each count, which is one bound value on
// Postgres and three on the positional dialects. The value bound under it is a
// tenancy.Scope, never a string derived from one: Scope is a driver.Valuer
// whose zero value fails at Value, so a scopeless read is a driver error rather
// than a wider result set.
var scopeMatch = querygen.Match{Column: queries.ScopeColumn}

// statements holds every rendered statement, keyed by what it does.
type statements struct {
	generator *querygen.Generator

	// listInvitationsBy is one rendered statement per column the paged
	// invitation reads key on, because the column is part of the statement and
	// there are exactly two of them. Rendering per call would put the choice on
	// the request path, where a column name is a value; rendering both here
	// keeps it a key into a map this package built.
	listInvitationsBy map[string]querygen.Bound

	createUser  querygen.Bound
	getUser     querygen.Bound
	listUsers   querygen.Bound
	updateUser  querygen.Bound
	archiveUser querygen.Bound

	createAccount  querygen.Bound
	getAccount     querygen.Bound
	listAccounts   querygen.Bound
	updateAccount  querygen.Bound
	archiveAccount querygen.Bound

	createInvitation querygen.Bound
	getInvitation    querygen.Bound
}

// pendingStatusMatch is the predicate both paged invitation reads carry.
//
// It was a literal inside a format string — status = 'pending' — and is now a
// column matched against a bound value, which renders identically and is one
// less place a quoted literal sits in SQL text.
var pendingStatusMatch = querygen.Match{Column: invitationStatusColumn}

// newStatements renders every conventional statement for one dialect against
// one set of prefixed table names.
func newStatements(d dialect.Dialect, t *tables) *statements {
	g := querygen.For(d)

	s := &statements{
		generator: g,

		createUser:  g.BoundCreate(t.users, queries.Users.InsertColumns(), queries.Users.Nullable),
		getUser:     g.BoundGet(t.users, queries.Users.Columns, scopeMatch),
		listUsers:   g.BoundList(t.users, queries.Users.Columns, scopeMatch),
		updateUser:  g.BoundUpdate(t.users, queries.Users.Columns, queries.Users.UpdateColumns(), queries.Users.Nullable, scopeMatch),
		archiveUser: g.BoundArchive(t.users, queries.Users.Columns, scopeMatch),

		createAccount:  g.BoundCreate(t.accounts, queries.Accounts.InsertColumns(), queries.Accounts.Nullable),
		getAccount:     g.BoundGet(t.accounts, queries.Accounts.Columns, scopeMatch),
		listAccounts:   g.BoundList(t.accounts, queries.Accounts.Columns, scopeMatch),
		updateAccount:  g.BoundUpdate(t.accounts, queries.Accounts.Columns, queries.Accounts.UpdateColumns(), queries.Accounts.Nullable, scopeMatch),
		archiveAccount: g.BoundArchive(t.accounts, queries.Accounts.Columns, scopeMatch),

		createInvitation: g.BoundCreate(t.invitations, queries.Invitations.InsertColumns(), queries.Invitations.Nullable),
		getInvitation:    g.BoundGet(t.invitations, queries.Invitations.Columns, scopeMatch),

		listInvitationsBy: map[string]querygen.Bound{},
	}

	for _, column := range []string{invitationFromUserColumn, invitationToEmailColumn} {
		s.listInvitationsBy[column] = g.BoundList(t.invitations, queries.Invitations.Columns,
			scopeMatch, querygen.Match{Column: column}, pendingStatusMatch)
	}

	return s
}

// argsFor resolves a rendered statement's positional arguments from values
// keyed by argument name, reporting the argument that had no value.
//
// [querygen.Bound.Bind] has no defaults, and that is the behavior worth
// keeping: every nullable argument here takes nil legitimately, so a missing
// name and an explicit nil are indistinguishable once bound. A miss is this
// package's bug rather than a caller's — nothing on a request path decides
// which arguments a statement has — so it is wrapped with the operation and
// reported rather than filled in.
func argsFor(statement querygen.Bound, values map[string]any, operation string) ([]any, error) {
	args, err := statement.Bind(values)
	if err != nil {
		return nil, platformerrors.Wrap(err, operation)
	}

	return args, nil
}
