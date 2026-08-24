package identity

import (
	"context"

	"github.com/primandproper/platform-go/v13/database"
	"github.com/primandproper/platform-go/v13/filtering"
	"github.com/primandproper/platform-go/v13/tenancy"
)

// Agreement names a document a user can accept.
//
// It is a closed set rather than a free string because the columns are fixed —
// a third document is a migration, not a new value — and because a typo in a
// string would record an acceptance nothing ever reads.
type Agreement string

const (
	// TermsOfService is the terms-of-service document.
	TermsOfService Agreement = "terms_of_service"

	// PrivacyPolicy is the privacy policy.
	PrivacyPolicy Agreement = "privacy_policy"
)

// Valid reports whether a is a known agreement.
func (a Agreement) Valid() bool {
	switch a {
	case TermsOfService, PrivacyPolicy:
		return true
	default:
		return false
	}
}

// Principal is a user together with the memberships that say what they may do:
// everything an authorization check, a session, or a request context needs
// about who is calling, read in one round trip.
//
// It exists because every application builds this by hand out of three queries
// on the hottest path it has — the one every authenticated request runs — and
// the hand-built version is where the active-account check gets forgotten. A
// Principal is only ever returned for an account the user is actually a member
// of; there is no way to obtain one that claims an account it has no membership
// in, which is the failure this type is shaped to prevent.
type Principal struct {
	_ struct{} `json:"-"`

	// User is who is calling, redacted — a Principal reaches a request context
	// and frequently a session store, and neither is a place for a password
	// hash. Read the credential fields through GetUser when a sign-in flow
	// genuinely needs them.
	User *User `json:"user"`

	// ActiveAccountID is the account this request is against: the one asked
	// for, or the user's default when none was.
	ActiveAccountID string `json:"activeAccountID"`

	// Memberships are every live membership the user holds, in this scope.
	Memberships []*Membership `json:"memberships"`
}

// ActiveMembership returns the membership for ActiveAccountID.
//
// It cannot be nil for a Principal a Store returned — resolving the active
// account is what the Store did to build one — so a nil here means the value
// was assembled by hand.
func (p *Principal) ActiveMembership() *Membership {
	if p == nil {
		return nil
	}

	for _, m := range p.Memberships {
		if m.BelongsToAccount == p.ActiveAccountID {
			return m
		}
	}

	return nil
}

// AccountRoles returns the role names the user holds in the active account.
func (p *Principal) AccountRoles() []string {
	if m := p.ActiveMembership(); m != nil {
		return m.Roles
	}

	return nil
}

// ServiceRoles returns the role names the user holds outside any account.
func (p *Principal) ServiceRoles() []string {
	if p == nil || p.User == nil {
		return nil
	}

	return p.User.ServiceRoles
}

// Roles returns every role name that applies to this request — the user's
// service roles followed by the roles they hold in the active account.
//
// It is what an authorization.PolicyResolver's PermissionsForRoles takes, and it
// is one method rather than two so that a caller cannot resolve permissions from
// half the answer. Passing only the account roles is the mistake that makes an
// operator's support access stop working inside a customer's account; passing
// only the service roles is the one that gives every user the same permissions
// everywhere.
func (p *Principal) Roles() []string {
	service, account := p.ServiceRoles(), p.AccountRoles()

	roles := make([]string, 0, len(service)+len(account))
	roles = append(roles, service...)
	roles = append(roles, account...)

	return roles
}

// AccountIDs returns every account the user belongs to, in the order the
// memberships were read — default account first.
func (p *Principal) AccountIDs() []string {
	if p == nil {
		return nil
	}

	ids := make([]string, 0, len(p.Memberships))
	for _, m := range p.Memberships {
		ids = append(ids, m.BelongsToAccount)
	}

	return ids
}

// Registrar writes the rows that make a new account exist.
//
// All three take the caller's database.SQLQueryExecutor and must run in one
// transaction: a user without an account signs in to nothing, and an account
// without an owner has no one its ownership checks can resolve to. That is why
// they are one interface rather than three methods spread across the reader and
// writer seams — the grouping is the invariant.
type Registrar interface {
	// CreateUser writes a new user through the caller's executor.
	//
	// It takes an executor because a registration is a user, an account, and a
	// membership, and a user who exists without an account is a user who signs
	// in to nothing. CreateAccount and CreateMembership take the same executor;
	// see the package documentation for the shape.
	//
	// The ID is generated if the user carries none, CreatedAt is stamped from
	// the Store's clock, and both are written back onto the value. A username or
	// email address already registered in this scope returns an error wrapping
	// ErrUsernameTaken or ErrEmailAddressTaken rather than a driver's constraint
	// violation — the caller's next move differs, and asking them to parse a
	// SQLSTATE to find out is how that check gets skipped.
	CreateUser(ctx context.Context, q database.SQLQueryExecutor, user *User) error

	// CreateAccount writes a new account through the caller's executor. The ID
	// is generated if the account carries none, and CreatedAt is stamped.
	CreateAccount(ctx context.Context, q database.SQLQueryExecutor, account *Account) error

	// CreateMembership puts a user in an account through the caller's executor.
	//
	// A user's first live membership becomes their default account whatever the
	// value says, because a user with memberships and no default has nowhere to
	// land — a state that is easy to write and confusing to debug. A subsequent
	// membership marked default moves the flag, which is what SetDefaultAccount
	// does and what accepting an invitation into a first account relies on.
	CreateMembership(ctx context.Context, q database.SQLQueryExecutor, membership *Membership) error
}

// CredentialStore is where the authentication engines put what they produce.
//
// This package never hashes, never compares, and never generates a TOTP secret;
// argon2, totp and webauthn do that and store nothing. These are the methods
// that persist their results, and they are separate from ProfileWriter for a
// reason that is a security property rather than tidiness: a credential must
// never be written by a read-modify-write over a whole User, because a caller
// writing back a value it read before a password rotation would restore the old
// hash. Every method here writes exactly one fact.
type CredentialStore interface {
	// GetUserByEmailVerificationToken reads the user a verification link names.
	// A token that has already been used matches nobody, because verifying
	// clears it.
	GetUserByEmailVerificationToken(ctx context.Context, scope tenancy.Scope, token string) (*User, error)

	// UpdateUserPassword replaces the stored hash, stamps PasswordLastChangedAt,
	// and clears RequiresPasswordChange.
	//
	// Clearing the flag here rather than leaving it to the caller is what makes
	// a forced password change terminate: a flow that set the flag, prompted,
	// wrote the new hash, and forgot to clear it prompts again forever.
	UpdateUserPassword(ctx context.Context, scope tenancy.Scope, userID, hashedPassword string) error

	// SetUserRequiresPasswordChange forces or releases a password change at next
	// sign-in.
	SetUserRequiresPasswordChange(ctx context.Context, scope tenancy.Scope, userID string, requires bool) error

	// UpdateUserTwoFactorSecret stores a new TOTP secret and marks it
	// unverified.
	//
	// Unverified is not optional and not a parameter. A newly issued secret has
	// by definition not been proven, and a method that let the caller say
	// otherwise would let a flow enroll a second factor nobody ever demonstrated
	// possession of.
	UpdateUserTwoFactorSecret(ctx context.Context, scope tenancy.Scope, userID, secret string) error

	// MarkUserTwoFactorSecretVerified records that the user proved possession of
	// the secret they hold.
	MarkUserTwoFactorSecretVerified(ctx context.Context, scope tenancy.Scope, userID string) error

	// SetUserEmailAddressVerificationToken stores the token a verification link
	// will carry, replacing any outstanding one — so re-sending a verification
	// email invalidates the previous link rather than leaving two live.
	SetUserEmailAddressVerificationToken(ctx context.Context, scope tenancy.Scope, userID, token string) error

	// MarkUserEmailAddressVerified stamps the address as proven and clears the
	// token, so the link cannot be replayed.
	//
	// The token is a parameter and is compared in the statement's predicate: the
	// caller has already read the user by token, and re-checking it here is what
	// makes a verification that raced another one write once.
	MarkUserEmailAddressVerified(ctx context.Context, scope tenancy.Scope, userID, token string) error
}

// SignInReader is the read side of authenticating a request.
//
// Two lookups by the handles a sign-in form submits, and the one read every
// authenticated request afterwards makes. It is deliberately the smallest
// interface here, because it is the one a middleware depends on, and a
// middleware that can only do these three things cannot accidentally do
// anything else.
type SignInReader interface {
	// GetUserByUsername reads a user by the handle they sign in with. Archived
	// users are excluded: this is the sign-in read, and a deleted account must
	// not authenticate.
	GetUserByUsername(ctx context.Context, scope tenancy.Scope, username string) (*User, error)

	// GetUserByEmailAddress is GetUserByUsername for the address. It is the read
	// a password-reset flow starts from.
	GetUserByEmailAddress(ctx context.Context, scope tenancy.Scope, emailAddress string) (*User, error)

	// GetPrincipal reads a user with their memberships and resolves which
	// account the request is against, in one round trip.
	//
	// An empty activeAccountID means the user's default account. A named one
	// must be an account the user is a live member of; otherwise the read
	// returns an error wrapping ErrMembershipNotFound rather than a Principal
	// with an account the caller asked for and has no right to. That check is
	// the reason this method exists rather than the caller joining the pieces:
	// it is the one every hand-built session context eventually forgets, and
	// forgetting it hands one account's data to another account's member.
	GetPrincipal(ctx context.Context, scope tenancy.Scope, userID, activeAccountID string) (*Principal, error)
}

// DirectoryReader reads the directory without changing it.
//
// Users, accounts, and the memberships between them, all scoped and all paged
// through filtering.QueryFilter where they return more than one row. Nothing
// here writes, so a console, an export, or a support tool can be handed this
// and nothing else.
type DirectoryReader interface {
	// GetUser reads one of the scope's users, credentials included. It returns
	// an error wrapping ErrUserNotFound when the user does not exist —
	// including when they exist in another scope, which is the same answer as
	// far as this scope is concerned.
	//
	// Archived users are returned. A soft-deleted user still appears in an audit
	// trail and in another domain's foreign key, and a read that hid them would
	// make those references dangle.
	GetUser(ctx context.Context, scope tenancy.Scope, userID string) (*User, error)

	// ListUsers pages the scope's directory, users redacted.
	ListUsers(ctx context.Context, scope tenancy.Scope, filter *filtering.QueryFilter) (*filtering.QueryFilteredResult[User], error)

	// ListUsersByIDs reads a batch of the scope's users in one query, redacted,
	// skipping IDs that name nobody in this scope.
	//
	// It exists because the alternative is a loop around GetUser, and every
	// application has the read that needs it: rendering a list where each row
	// names the user who created it. A partial answer rather than an error for a
	// missing ID is deliberate — the caller is hydrating references, and one
	// deleted author should not empty the page.
	ListUsersByIDs(ctx context.Context, scope tenancy.Scope, userIDs []string) ([]*User, error)

	// SearchUsersByUsername pages the users in scope whose username begins with
	// prefix, redacted.
	//
	// A prefix match rather than a substring one, because a prefix uses the
	// index on (scope, username) and a substring cannot. An application that
	// needs fuzzy search over its directory wants this module's search package
	// pointed at it, not a LIKE '%x%' that scans the table on every keystroke.
	SearchUsersByUsername(ctx context.Context, scope tenancy.Scope, prefix string, filter *filtering.QueryFilter) (*filtering.QueryFilteredResult[User], error)

	// GetAccount reads one of the scope's accounts, returning an error wrapping
	// ErrAccountNotFound when it does not exist here.
	//
	// Archived accounts are returned, as archived users are and for the same
	// reason: a soft-deleted account is still named by an audit row, an invoice,
	// and another domain's foreign key, and a read that hid it would make those
	// references dangle. Callers rendering an account check Account.Archived.
	GetAccount(ctx context.Context, scope tenancy.Scope, accountID string) (*Account, error)

	// ListAccounts pages the scope's accounts.
	ListAccounts(ctx context.Context, scope tenancy.Scope, filter *filtering.QueryFilter) (*filtering.QueryFilteredResult[Account], error)

	// ListAccountsForUser pages the accounts a user is a live member of.
	ListAccountsForUser(ctx context.Context, scope tenancy.Scope, userID string, filter *filtering.QueryFilter) (*filtering.QueryFilteredResult[Account], error)

	// GetMembership reads the live membership between a user and an account,
	// returning an error wrapping ErrMembershipNotFound when there is none.
	GetMembership(ctx context.Context, scope tenancy.Scope, userID, accountID string) (*Membership, error)

	// ListMembershipsForUser returns every live membership a user holds, default
	// account first.
	//
	// It returns a slice rather than a page because a user belongs to a handful
	// of accounts, and paging a handful means a caller who forgets to loop reads
	// some of somebody's memberships and authorizes against the rest as if they
	// did not exist.
	ListMembershipsForUser(ctx context.Context, scope tenancy.Scope, userID string) ([]*Membership, error)

	// ListAccountMembers pages an account's roster, each membership joined to
	// the redacted user it names.
	ListAccountMembers(ctx context.Context, scope tenancy.Scope, accountID string, filter *filtering.QueryFilter) (*filtering.QueryFilteredResult[MembershipWithUser], error)
}

// ProfileWriter writes what a user or an account may change about itself.
//
// Names, addresses, time zones, and the record of an accepted document — the
// fields where losing a concurrent write costs a stale display name rather than
// a security regression. What is deliberately not reachable from here:
// credentials (CredentialStore), billing state (BillingWriter), account
// ownership (MembershipWriter), and status (AdminWriter). UpdateUser and
// UpdateAccount ignore those columns entirely rather than trusting the caller
// to have read them recently.
type ProfileWriter interface {
	// UpdateUser writes the user's profile: username, email address, and names.
	//
	// It cannot write a credential, deliberately. A caller holding a User read
	// some time ago and writing it back would otherwise restore a password that
	// has since been changed — the classic read-modify-write, on the one field
	// where losing the newer value is a security regression rather than a stale
	// display name. Credentials move through UpdateUserPassword,
	// UpdateUserTwoFactorSecret, and their siblings.
	//
	// Changing the username or email address to one already registered in this
	// scope returns ErrUsernameTaken or ErrEmailAddressTaken. Changing the email
	// address clears its verification: the new address has not been proven.
	UpdateUser(ctx context.Context, user *User) error

	// UpdateAccount writes the account's name and billing address.
	//
	// It writes neither the billing state nor the owner, for the same reason
	// UpdateUser writes no credential: both are changed by flows that do not
	// hold the rest of the account, and a read-modify-write over them loses
	// whatever a processor webhook or an ownership transfer did in between. See
	// UpdateAccountBilling and TransferAccountOwnership.
	UpdateAccount(ctx context.Context, account *Account) error

	// RecordAgreement stamps the user's acceptance of one or more documents, as
	// of the Store's clock.
	//
	// It uses the Store's own handle rather than the caller's executor, so it
	// cannot join a registration transaction. A registration that collects
	// acceptance at sign-up should set LastAcceptedTermsOfService and
	// LastAcceptedPrivacyPolicy on the User instead — CreateUser writes them
	// with the row. This method is for the later acceptance, when a new version
	// of a document is published and the user agrees to it in a request of its
	// own.
	RecordAgreement(ctx context.Context, scope tenancy.Scope, userID string, agreements ...Agreement) error
}

// MembershipWriter changes who belongs to an account and what they may do
// there.
//
// This is the authorization-shaped half of the store: the roles these methods
// write are the roles a PolicyResolver later reads. It is separated from
// AdminWriter because the two answer to different people — an account
// administrator manages their own roster, an operator manages the deployment —
// and a service that exposes the first should not be holding the second.
type MembershipWriter interface {
	// SetMembershipRoles replaces the roles a user holds in an account.
	//
	// It replaces rather than merges. A caller adding a role reads the
	// membership and writes the union, which is visible in their code; a merging
	// setter makes removing a role impossible through the same method and is the
	// reason revocation flows end up issuing raw SQL.
	SetMembershipRoles(ctx context.Context, scope tenancy.Scope, userID, accountID string, roles []string) error

	// SetDefaultAccount marks one of a user's accounts as the one they land in,
	// clearing the flag from the others in the same transaction — the invariant
	// being one default per user, not one per call.
	SetDefaultAccount(ctx context.Context, scope tenancy.Scope, userID, accountID string) error

	// TransferAccountOwnership moves an account to a new owner, giving the new
	// owner a membership if they lack one and leaving the old owner's in place.
	//
	// The old owner keeps their membership deliberately: transferring ownership
	// and ejecting somebody are different acts, and doing both here would make
	// the common case — handing over to a colleague and staying on — impossible
	// to express.
	TransferAccountOwnership(ctx context.Context, scope tenancy.Scope, accountID, newOwnerUserID string) error

	// RemoveMembership ends a user's membership in an account.
	//
	// Removing the account's owner returns an error wrapping
	// ErrLastAccountOwner. An ownerless account fails every permission check
	// that resolves through its owner, and the failure surfaces far from the
	// removal that caused it. Transfer ownership first.
	//
	// Removing a user's default account moves the default to another live
	// membership, so a user is never left with memberships and nowhere to land.
	RemoveMembership(ctx context.Context, scope tenancy.Scope, userID, accountID string) error
}

// AdminWriter is the operator's half: the writes an account holder cannot make
// about themselves.
//
// Banning, terminating, granting a service role, soft-deleting, and erasing.
// These are the methods whose exposure through an ordinary request handler is a
// privilege escalation, so they are named as a group precisely to make that
// exposure a deliberate act rather than a consequence of depending on Store.
type AdminWriter interface {
	// UpdateUserAccountStatus moves a user between statuses, recording the
	// explanation shown to them.
	UpdateUserAccountStatus(ctx context.Context, scope tenancy.Scope, userID string, status AccountStatus, explanation string) error

	// SetUserServiceRoles replaces the roles a user holds outside any account.
	//
	// It replaces rather than merges, for the reason SetMembershipRoles does: a
	// merging setter cannot revoke, and revocation is the operation that matters
	// most on the role set that grants operator access. An empty set is allowed
	// here where an empty membership role set is not — a user with no service
	// roles is the ordinary case, and it is how an operator's access is
	// withdrawn.
	SetUserServiceRoles(ctx context.Context, scope tenancy.Scope, userID string, roles []string) error

	// ArchiveUser soft-deletes a user and ends every membership they hold, in
	// one transaction. A user archived with live memberships would still appear
	// in the accounts they belonged to, which is the state an application
	// discovers when a deleted colleague is still on the roster.
	ArchiveUser(ctx context.Context, scope tenancy.Scope, userID string) error

	// EraseUser destroys the user row through the caller's executor, returning
	// how many rows went.
	//
	// It takes an executor rather than a handle of its own because a
	// right-to-be-forgotten erasure shares one transaction with every other
	// domain's eraser and with the record that the erasure happened. A subject
	// erased from the directory and present in another domain's table has no
	// coherent status, so the whole thing has to be able to roll back together.
	// See this module's dataprivacy package.
	EraseUser(ctx context.Context, q database.SQLQueryExecutor, scope tenancy.Scope, userID string) (int64, error)

	// ArchiveAccount soft-deletes an account and ends every membership in it, in
	// one transaction.
	ArchiveAccount(ctx context.Context, scope tenancy.Scope, accountID string) error
}

// BillingWriter is what a payment processor's webhook handler needs, and
// nothing else.
//
// One method, because that is genuinely the whole surface: a processor tells
// this application that an account's standing changed, and this is where that
// lands. A webhook endpoint is unauthenticated and public, so what it can reach
// is worth being able to state in one line.
type BillingWriter interface {
	// UpdateAccountBilling writes only the billing fields the update names, so a
	// processor webhook carrying a status alone does not have to read the rest
	// first.
	UpdateAccountBilling(ctx context.Context, scope tenancy.Scope, accountID string, update *BillingUpdate) error
}

// InvitationStore covers an invitation's whole life: issued, looked up,
// answered.
//
// AcceptInvitation takes the caller's executor because accepting is a
// membership write and a status write that have to commit together, and it
// takes the roles off the invitation rather than from a parameter — what
// somebody was invited to is what they get.
type InvitationStore interface {
	// CreateInvitation writes an invitation. The ID is generated if it carries
	// none, and CreatedAt is stamped.
	CreateInvitation(ctx context.Context, invitation *Invitation) error

	// GetInvitation reads one of the scope's invitations by ID, for the sender
	// looking at what they have sent.
	GetInvitation(ctx context.Context, scope tenancy.Scope, invitationID string) (*Invitation, error)

	// GetInvitationByToken reads the invitation a link names, comparing the
	// token.
	//
	// The ID and the token are both required, and the ID is what the row is
	// found by. Looking up by token alone would make the token an index key —
	// which is a secret in an index and a timing signal on every miss — where
	// naming the row first means one constant-time-ish comparison against one
	// value.
	//
	// An expired invitation returns an error wrapping ErrInvitationExpired
	// rather than ErrInvitationNotFound, so the recipient can be told to ask for
	// another rather than that their link was wrong.
	GetInvitationByToken(ctx context.Context, scope tenancy.Scope, invitationID, token string) (*Invitation, error)

	// ListInvitationsFromUser pages the pending invitations a user has sent,
	// redacted.
	ListInvitationsFromUser(ctx context.Context, scope tenancy.Scope, userID string, filter *filtering.QueryFilter) (*filtering.QueryFilteredResult[Invitation], error)

	// ListInvitationsForEmailAddress pages the pending invitations addressed to
	// an email address, redacted — what a newly registered user is shown.
	ListInvitationsForEmailAddress(ctx context.Context, scope tenancy.Scope, emailAddress string, filter *filtering.QueryFilter) (*filtering.QueryFilteredResult[Invitation], error)

	// AcceptInvitation marks an invitation accepted and writes the membership it
	// promised, through the caller's executor, returning that membership.
	//
	// The two are one call rather than two because an accepted invitation
	// without a membership is a user who was told they joined and did not, and
	// the executor is the caller's because a registration that accepts an
	// invitation writes the user in the same transaction.
	//
	// The roles come from the invitation rather than from a parameter: what
	// somebody was invited to is what they get, and a parameter here is where an
	// escalation goes in.
	AcceptInvitation(ctx context.Context, q database.SQLQueryExecutor, scope tenancy.Scope, invitationID, token, acceptingUserID, note string) (*Membership, error)

	// SetInvitationStatus answers an invitation without producing a membership:
	// rejection by the recipient, cancellation by the sender.
	//
	// It refuses InvitationAccepted, returning ErrInvalidInvitationStatus —
	// accepting is AcceptInvitation, and a status write that produced no
	// membership would leave exactly the state that method exists to prevent.
	SetInvitationStatus(ctx context.Context, scope tenancy.Scope, invitationID string, status InvitationStatus, note string) error
}

// Store is the whole persistence seam for the identity directory: every
// interface above, in one name.
//
// This package ships a SQL implementation (NewSQLStore) together with the DDL
// it needs (identity/migrations), so adopting identity does not mean writing
// this. The interface exists because an application with its own schema
// conventions — or one whose directory is LDAP, or an upstream identity
// provider it mirrors — should not have to fork the package to keep them.
//
// # Depend on a narrower one
//
// Store is forty-three methods, which is the right size for the thing that
// implements it and the wrong size for almost everything that calls it. It is a
// union of nine interfaces, each named for a job rather than for a table, and a
// caller should name the smallest one that covers what it does: a sign-in
// middleware takes a SignInReader, a processor webhook takes a BillingWriter, a
// support console takes a DirectoryReader.
//
// This is not only about the size of a test double, though a three-method fake
// beats a forty-three-method mock. It is that the narrow interface is a
// statement about reach, checked by the compiler: a handler that holds a
// DirectoryReader cannot ban a user, and one that holds a ProfileWriter cannot
// write a credential or move an account's ownership. Depending on Store gives
// away all of that at once, so it is what a container registers and a store
// implements, not what a handler asks for.
//
// A SQL implementation is deliberately one type behind all nine. The writes
// that make a registration span three tables in one transaction, so splitting
// the implementation would split a transaction; only the seam divides.
//
// Methods taking a database.SQLQueryExecutor run inside the caller's
// transaction and must use it rather than a handle of their own. Those are
// exactly the writes that have to commit with something else: the three that
// make a registration, the two that make an accepted invitation, and the
// erasure that spans every domain a subject appears in. The rest own their own
// statements.
//
// Every method takes a tenancy.Scope or reads one off the value it was handed,
// and none of them offers an unscoped variant. There is no ListAllUsers, and
// the absence is deliberate: an operator console listing every user in a
// deployment is listing one directory at a time, and the method that spans
// directories is the one whose missing predicate serves one customer's user
// list to another.
type Store interface {
	Registrar
	CredentialStore
	SignInReader
	DirectoryReader
	ProfileWriter
	MembershipWriter
	AdminWriter
	BillingWriter
	InvitationStore
}
