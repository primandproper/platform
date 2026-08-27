package identity

import (
	platformerrors "github.com/primandproper/platform-go/v13/errors"
)

// The sentinels this package returns. They live together because a caller
// deciding what to do next is choosing between them, and a set spread across
// the files that happen to return each one cannot be read as the set it is.
var (
	// ErrNilDatabaseClient indicates a nil database.Client. It wraps
	// errors.ErrNilInputParameter, so a caller may check either.
	ErrNilDatabaseClient = platformerrors.Wrap(platformerrors.ErrNilInputParameter, "nil database client")

	// ErrNilExecutor indicates a nil database.SQLQueryExecutor handed to one of
	// the methods that run inside the caller's transaction.
	ErrNilExecutor = platformerrors.Wrap(platformerrors.ErrNilInputParameter, "nil query executor")

	// ErrNilUser indicates a nil *User where one was required.
	ErrNilUser = platformerrors.Wrap(platformerrors.ErrNilInputParameter, "nil user")

	// ErrNilAccount indicates a nil *Account where one was required.
	ErrNilAccount = platformerrors.Wrap(platformerrors.ErrNilInputParameter, "nil account")

	// ErrNilMembership indicates a nil *Membership where one was required.
	ErrNilMembership = platformerrors.Wrap(platformerrors.ErrNilInputParameter, "nil membership")

	// ErrNilInvitation indicates a nil *Invitation where one was required.
	ErrNilInvitation = platformerrors.Wrap(platformerrors.ErrNilInputParameter, "nil invitation")

	// ErrUsernameTaken indicates a username already registered in this scope.
	//
	// It is a distinct error rather than a raw constraint violation because
	// registration is the one flow where the difference between "your input
	// collides" and "the database is unwell" decides whether the caller retries
	// or reports.
	ErrUsernameTaken = platformerrors.New("username is already registered")

	// ErrEmailAddressTaken indicates an email address already registered in this
	// scope.
	ErrEmailAddressTaken = platformerrors.New("email address is already registered")

	// ErrUserNotFound indicates a user that does not exist in the scope that
	// asked. A user in another scope reads as absent, which is what it is from
	// here — and is the answer that does not turn the read into an oracle for
	// which usernames exist in other directories.
	ErrUserNotFound = platformerrors.New("user not found")

	// ErrAccountNotFound indicates an account that does not exist in the scope
	// that asked.
	ErrAccountNotFound = platformerrors.New("account not found")

	// ErrMembershipNotFound indicates a (user, account) pair with no live
	// membership between them in this scope.
	ErrMembershipNotFound = platformerrors.New("membership not found")

	// ErrInvitationNotFound indicates an invitation that does not exist, has
	// expired, or has already been answered.
	ErrInvitationNotFound = platformerrors.New("invitation not found")

	// ErrNoDefaultAccount indicates a user with no account marked as their
	// default, which for a user created through CreateMembership cannot happen —
	// the first membership is the default. It surfaces for a directory whose
	// rows were written by something else.
	ErrNoDefaultAccount = platformerrors.New("user has no default account")

	// ErrLastAccountOwner indicates an act that would leave an account without
	// an owner: removing the owner's membership, or archiving the owner
	// themselves. An ownerless account is unreachable by every permission check
	// that resolves through its owner, so both are refused rather than leaving
	// one behind, and the error names the account that has to move first.
	//
	// EraseUser is the one path that reaches the same state and does not return
	// this, because it cannot: see Store.EraseUser.
	ErrLastAccountOwner = platformerrors.New("cannot remove the last owner of an account")

	// ErrInvitationExpired indicates an invitation whose ExpiresAt has passed.
	// It is distinct from ErrInvitationNotFound so that the recipient can be
	// told to ask for another one rather than that the link was wrong.
	ErrInvitationExpired = platformerrors.New("invitation has expired")

	// ErrInvalidEmailAddress indicates an address net/mail cannot parse. It
	// wraps errors.ErrUnrecognizedInputValue, so a caller may check either.
	ErrInvalidEmailAddress = platformerrors.Wrap(platformerrors.ErrUnrecognizedInputValue, "invalid email address")

	// ErrInvalidTimeZone indicates a time zone name the runtime cannot load.
	// It wraps errors.ErrUnrecognizedInputValue, so a caller may check either.
	ErrInvalidTimeZone = platformerrors.Wrap(platformerrors.ErrUnrecognizedInputValue, "invalid time zone")

	// ErrInvalidInvitationStatus indicates a status write SetInvitationStatus
	// will not perform — today, InvitationAccepted. It wraps
	// errors.ErrUnrecognizedInputValue, so a caller may check either.
	ErrInvalidInvitationStatus = platformerrors.Wrap(
		platformerrors.ErrUnrecognizedInputValue,
		"invitation status cannot be set directly",
	)
)
