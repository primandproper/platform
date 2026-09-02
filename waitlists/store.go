package waitlists

import (
	"context"

	"github.com/primandproper/platform-go/v14/filtering"
	"github.com/primandproper/platform-go/v14/tenancy"
)

// Store is the whole of what this package persists: the lists, and the people
// queueing on them.
//
// It is two interfaces because they have two callers. A ListStore is reached by
// whatever administers a deployment — an admin console, a seeding job, whoever
// decides a launch is happening — and a SignupStore is reached on the request
// path, by the form somebody fills in and by the operator working through the
// queue. Splitting them is what lets a component depend on the half it uses;
// Store is here for the wiring that provides both.
type Store interface {
	ListStore
	SignupStore
}

// ListStore is the catalog: what lists exist, what they are for, and when each
// stops taking signups.
//
// Every method takes a tenancy.Scope, and a deployment with one catalog passes
// tenancy.Global() to all of them. There is deliberately no unscoped variant of
// any of these — see the tenancy package.
type ListStore interface {
	// CreateList opens a waitlist and returns it as stored, with the id it was
	// minted under and the creation time the database assigned.
	//
	// It refuses a list with no name and one with no closing time. There is no
	// default for the latter — see ErrEmptyClosesAt.
	CreateList(ctx context.Context, scope tenancy.Scope, list *List) (*List, error)

	// GetList reads one live list by id.
	GetList(ctx context.Context, scope tenancy.Scope, listID string) (*List, error)

	// ListLists pages the scope's catalog, open and closed alike. It is the
	// administrative read.
	ListLists(ctx context.Context, scope tenancy.Scope, filter *filtering.QueryFilter) (*filtering.QueryFilteredResult[List], error)

	// ListOpenLists pages the lists still taking signups: live, and not yet
	// past their closing time as of the store's clock.
	//
	// It is the public read — what a "join the waitlist" page offers — and it
	// is a separate statement rather than a filter applied to ListLists'
	// results, because a page filtered after the fact is a page whose size the
	// caller cannot rely on.
	ListOpenLists(ctx context.Context, scope tenancy.Scope, filter *filtering.QueryFilter) (*filtering.QueryFilteredResult[List], error)

	// UpdateList rewrites a list's name, description and closing time.
	//
	// Moving the closing time is how a list is extended or brought in, and it
	// is not guarded against the signups already on it: a list closed early
	// keeps everybody who joined while it was open, and reopening one lets the
	// next person through. What it will not do is revive an archived list.
	UpdateList(ctx context.Context, scope tenancy.Scope, list *List) error

	// ArchiveList retires a list.
	//
	// The signups against it are left alone and stay readable, because archiving
	// is not erasure. What it does do is close the list to new signups
	// immediately, whatever its closing time says — see List.OpenAt.
	ArchiveList(ctx context.Context, scope tenancy.Scope, listID string) error
}

// SignupStore is the queue: who is on a list, where they stand, and what
// happens when they ask to come off it.
//
// Every method takes both the scope and the list. The list is half of what
// addresses a signup, and a read that omitted it would be a read that could
// hand one list's row to a caller holding another list's id.
type SignupStore interface {
	// Join adds somebody to a list.
	//
	// It refuses a list that is closed or missing (ErrListClosed,
	// ErrListNotFound), a contact already on the list (ErrAlreadySignedUp), and
	// a contact that has withdrawn from it (ErrContactWithdrawn) — the last of
	// which is the obligation this package is shaped around and outlives the
	// address it is about.
	//
	// The signup's Contact is stored as it was given and digested as
	// [Normalize] renders it, so two capitalizations of one address are one
	// person. Status and the timestamps are the store's; whatever the caller
	// set on them is ignored.
	Join(ctx context.Context, scope tenancy.Scope, listID string, signup *Signup) (*Signup, error)

	// GetSignup reads one live signup by id, on the list it belongs to.
	GetSignup(ctx context.Context, scope tenancy.Scope, listID, signupID string) (*Signup, error)

	// GetSignupByContact reads one live signup by the address it was made with,
	// which is the read behind "am I already on this list" and the read an
	// unsubscribe link resolves.
	//
	// The contact is normalized and digested, so it is found by whichever
	// capitalization the caller has. A withdrawn signup is still a live row and
	// comes back as one, with its contact blank and its status saying why —
	// which is what lets an unsubscribe page tell somebody they are already off
	// the list rather than that they were never on it.
	GetSignupByContact(ctx context.Context, scope tenancy.Scope, listID, contact string) (*Signup, error)

	// ListSignups pages one list's live signups, oldest first by default, which
	// is the order they joined in.
	ListSignups(ctx context.Context, scope tenancy.Scope, listID string, filter *filtering.QueryFilter) (*filtering.QueryFilteredResult[Signup], error)

	// ListSignupsForSubject pages the signups belonging to one principal across
	// every list in the scope. It is the read a profile page makes and the one a
	// data privacy export walks.
	//
	// A withdrawn signup is not among them: a withdrawal blanks the subject
	// reference along with the contact, so the row that remembers a suppression
	// no longer says whose it was.
	ListSignupsForSubject(ctx context.Context, scope tenancy.Scope, subject Subject, filter *filtering.QueryFilter) (*filtering.QueryFilteredResult[Signup], error)

	// UpdateSignupNotes rewrites the operator's note against a signup.
	//
	// It is the one write that touches a signup without moving it, and it
	// deliberately leaves StatusChangedAt alone — a typo fixed in a note must
	// not reschedule the reminder somebody's invitation started.
	UpdateSignupNotes(ctx context.Context, scope tenancy.Scope, listID, signupID, notes string) error

	// Invite moves a waiting signup to invited and stamps the moment.
	//
	// It refuses anything that is not waiting with ErrWrongStatus, and the
	// refusal is the affected-row count of a guarded update rather than a
	// decision made on a read — so two requests inviting the same person send
	// one email between them.
	Invite(ctx context.Context, scope tenancy.Scope, listID, signupID string) error

	// Convert moves an invited signup to converted and stamps the moment. It
	// refuses anything that is not invited with ErrWrongStatus, guarded the same
	// way Invite is.
	Convert(ctx context.Context, scope tenancy.Scope, listID, signupID string) error

	// Withdraw takes somebody off the list at their own request, and erases what
	// the row said about them.
	//
	// It blanks the contact, the notes and the subject reference and keeps the
	// contact digest, which is what lets a later signup from the same address be
	// refused with ErrContactWithdrawn instead of quietly re-subscribing
	// somebody who asked to be left alone. It moves a signup in any status; a
	// second call reports ErrAlreadyWithdrawn rather than restamping the moment
	// they left.
	Withdraw(ctx context.Context, scope tenancy.Scope, listID, signupID string) error

	// ArchiveSignup retires a signup administratively.
	//
	// It is not a withdrawal and must not be used as one: it hides the row from
	// every read that does not ask for archived rows and changes nothing about
	// what the row holds, so the contact is still stored and nothing suppresses
	// a re-signup — the uniqueness covers archived rows, so what the next
	// attempt gets is ErrAlreadySignedUp. Somebody asking to come off a list
	// wants Withdraw.
	ArchiveSignup(ctx context.Context, scope tenancy.Scope, listID, signupID string) error
}
