package waitlists

import (
	platformerrors "github.com/primandproper/platform-go/v14/errors"
)

// The sentinels this package returns. They live together because a caller
// deciding what to do next is choosing between them, and a set spread across the
// files that happen to return each one cannot be read as the set it is.
var (
	// ErrNilDatabaseClient indicates a nil database.Client. It wraps
	// errors.ErrNilInputParameter, so a caller may check either.
	ErrNilDatabaseClient = platformerrors.Wrap(platformerrors.ErrNilInputParameter, "nil database client")

	// ErrNilList indicates a nil *List where one was required.
	ErrNilList = platformerrors.Wrap(platformerrors.ErrNilInputParameter, "nil waitlist")

	// ErrNilSignup indicates a nil *Signup where one was required.
	ErrNilSignup = platformerrors.Wrap(platformerrors.ErrNilInputParameter, "nil waitlist signup")

	// ErrEmptyListName indicates a list with no name. A list nobody can name is
	// a list nobody can administer, since the id is minted by the store.
	ErrEmptyListName = platformerrors.Wrap(platformerrors.ErrEmptyInputParameter, "empty waitlist name")

	// ErrEmptyClosesAt indicates a list with no closing time.
	//
	// It is refused rather than defaulted, because every default available is a
	// policy: an hour is too short for anything, a decade is a list nobody will
	// ever close, and "never" is the state the column deliberately cannot hold.
	// See waitlists/migrations.
	ErrEmptyClosesAt = platformerrors.Wrap(platformerrors.ErrEmptyInputParameter, "waitlist has no closing time")

	// ErrEmptyContact indicates a signup with no contact, or one that is
	// nothing but whitespace. The address is what the list exists to hold.
	ErrEmptyContact = platformerrors.Wrap(platformerrors.ErrEmptyInputParameter, "empty waitlist signup contact")

	// ErrEmptySubjectType indicates a Subject naming an id and no type.
	ErrEmptySubjectType = platformerrors.Wrap(platformerrors.ErrEmptyInputParameter, "empty subject type")

	// ErrEmptySubjectID indicates a Subject naming a type and no id.
	ErrEmptySubjectID = platformerrors.Wrap(platformerrors.ErrEmptyInputParameter, "empty subject id")

	// ErrListNotFound indicates no live list by that id in this scope. Every
	// signup-side call can return it, because a signup is only meaningful
	// against a list.
	ErrListNotFound = platformerrors.New("waitlist not found")

	// ErrSignupNotFound indicates no live signup by that id on that list in
	// this scope.
	ErrSignupNotFound = platformerrors.New("waitlist signup not found")

	// ErrListClosed indicates a signup for a list that has stopped taking them:
	// past its closing time, or archived.
	//
	// It is a distinct error rather than a not-found, because the two lead a
	// caller somewhere different — a closed list is a page that says "we are no
	// longer taking signups", and a missing one is a broken link.
	ErrListClosed = platformerrors.New("waitlist is closed")

	// ErrAlreadySignedUp indicates a contact that is already on this list.
	//
	// It is a distinct error rather than a raw constraint violation because the
	// difference between "your input collides" and "the database is unwell"
	// decides whether the caller reports to a person or retries. It covers
	// archived signups too: the uniqueness does, so a name freed by archiving is
	// a name that stays taken.
	ErrAlreadySignedUp = platformerrors.New("contact is already on this waitlist")

	// ErrContactWithdrawn indicates a signup from a contact that has asked to
	// come off this list.
	//
	// It is the reason the digest outlives the address. A withdrawal that let
	// the next signup through would be an unsubscribe that lasted until somebody
	// filled the form in again, which is not an unsubscribe — so the row stays,
	// keyed on a digest of an address the table no longer holds, and this is
	// what a later attempt gets. A person who genuinely wants back on is put
	// back by whoever administers the list, which is a deliberate act rather
	// than a form submission.
	ErrContactWithdrawn = platformerrors.New("contact has withdrawn from this waitlist")

	// ErrWrongStatus indicates a transition from a status the signup is not in:
	// converting somebody who was never invited, inviting somebody twice.
	//
	// It is the affected-row count of a guarded update rather than a decision
	// made on a read, which is what makes a transition happen exactly once. Two
	// requests inviting the same signup both find it waiting; only one of their
	// updates reports a row, and the other is told this.
	ErrWrongStatus = platformerrors.New("waitlist signup is not in the status the transition requires")

	// ErrAlreadyWithdrawn indicates a withdrawal of a signup that has already
	// been withdrawn.
	//
	// A replay reports it rather than restamping, because the moment somebody
	// asked to come off a list is a fact about them and a second request should
	// not move it.
	ErrAlreadyWithdrawn = platformerrors.New("waitlist signup has already been withdrawn")
)
