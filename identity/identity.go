package identity

import (
	"net/mail"
	"time"

	platformerrors "github.com/primandproper/platform-go/v13/errors"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

// serviceName scopes this package's spans, logger, and instruments.
const serviceName = "identity"

// The keys this package attaches to spans and log lines. Declared once so a
// trace and a log line name the same fact the same way.
const (
	scopeKey        = "identity.scope"
	userIDKey       = "identity.user_id"
	usernameKey     = "identity.username"
	accountIDKey    = "identity.account_id"
	invitationIDKey = "identity.invitation_id"
	countKey        = "identity.count"

	// storeOpKey labels the unreported-row-count counter with the write that
	// could not confirm itself.
	storeOpKey = "identity.operation"
)

// UserAttributeKey is the metric and span attribute a caller labels its own
// instruments with when the thing being measured is about one user. It is
// exported so a consumer's attributes agree with this package's rather than
// merely resembling them.
const UserAttributeKey = userIDKey

// AccountAttributeKey is UserAttributeKey for accounts.
const AccountAttributeKey = accountIDKey

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

	// ErrLastAccountOwner indicates an attempt to remove the only owner of an
	// account. An ownerless account is unreachable by every permission check
	// that resolves through its owner, so the removal is refused rather than
	// leaving one behind.
	ErrLastAccountOwner = platformerrors.New("cannot remove the last owner of an account")

	// ErrInvitationExpired indicates an invitation whose ExpiresAt has passed.
	// It is distinct from ErrInvitationNotFound so that the recipient can be
	// told to ask for another one rather than that the link was wrong.
	ErrInvitationExpired = platformerrors.New("invitation has expired")
)

// ErrInvalidEmailAddress indicates an address net/mail cannot parse. It wraps
// errors.ErrUnrecognizedInputValue, so a caller may check either.
var ErrInvalidEmailAddress = platformerrors.Wrap(platformerrors.ErrUnrecognizedInputValue, "invalid email address")

// ErrInvalidTimeZone indicates a time zone name the runtime cannot load. It
// wraps errors.ErrUnrecognizedInputValue, so a caller may check either.
var ErrInvalidTimeZone = platformerrors.Wrap(platformerrors.ErrUnrecognizedInputValue, "invalid time zone")

// timeZoneRule validates an IANA time zone name by loading it.
//
// Loading rather than pattern-matching, because the only useful definition of a
// valid zone name is one the runtime can turn into a *time.Location — and a
// name that looks right but does not load ("America/Chicagoo") renders every
// date on the account wrong, forever, without anything saying so. Failing the
// write is how that stays a typo instead of becoming data.
//
// It has a runtime cost worth knowing about: any zone but UTC needs the
// zoneinfo database, which scratch and distroless images do not carry, and
// without it this rejects names that are perfectly good elsewhere. That is the
// same trade the jobs package documents at length for cron schedules, and the
// same fix applies — `import _ "time/tzdata"` in the binary's main package
// embeds it.
//
// "Local" is refused rather than loaded. Go builds time.Local from the
// process's TZ environment variable, so a stored "Local" means whatever the
// reader's host happens to think — which makes two replicas of one service
// disagree about when an account's day starts, and makes a value written on a
// laptop mean something else in production.
var timeZoneRule = validation.By(func(value any) error {
	name, ok := value.(string)
	if !ok || name == "" {
		// Empty is "not stated", which is a legitimate answer — see
		// Account.Location for what reads it.
		return nil
	}

	if name == "Local" {
		return platformerrors.Wrap(ErrInvalidTimeZone, `"Local" names the reader's host rather than a zone`)
	}

	if _, err := time.LoadLocation(name); err != nil {
		return platformerrors.Wrapf(ErrInvalidTimeZone, "%q: %v", name, err)
	}

	return nil
})

// emailAddressRule is the validation both a User and an Invitation apply to an
// address, written once because it is a rule that can be got wrong twice — and
// the copy that drifts is the one that lets an unreachable address into a
// directory where it is also a unique key.
//
// It parses with net/mail rather than matching a pattern. A regular expression
// for RFC 5322 is either wrong or unreadable, and the standard library already
// holds the grammar that the email package formats addresses against — so an
// address this accepts is one email.FormatAddress can render.
//
// It deliberately does not check that the domain resolves. That is a network
// call on a validation path, it is wrong the moment DNS changes, and the only
// proof an address is reachable is having sent to it — which is what the
// verification token exists for.
var emailAddressRule = validation.By(func(value any) error {
	address, ok := value.(string)
	if !ok || address == "" {
		// Emptiness is validation.Required's to report, so that a missing
		// address and a malformed one do not both come back as "invalid".
		return nil
	}

	parsed, err := mail.ParseAddress(address)
	if err != nil {
		return platformerrors.Wrapf(ErrInvalidEmailAddress, "%q: %v", address, err)
	}

	// ParseAddress accepts a display name — `Ada <ada@example.com>` — and this
	// column holds an address alone. Accepting the longer form would store a
	// value that is a unique key, is compared against what a sign-in form
	// submits, and does not equal it.
	if parsed.Address != address {
		return platformerrors.Wrapf(ErrInvalidEmailAddress, "%q is not a bare address", address)
	}

	return nil
})
