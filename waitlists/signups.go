package waitlists

import (
	"context"
	"database/sql"
	"errors"

	"github.com/primandproper/platform-go/v14/database"
	platformerrors "github.com/primandproper/platform-go/v14/errors"
	"github.com/primandproper/platform-go/v14/filtering"
	"github.com/primandproper/platform-go/v14/identifiers"
	"github.com/primandproper/platform-go/v14/observability"
	"github.com/primandproper/platform-go/v14/tenancy"
	"github.com/primandproper/platform-go/v14/waitlists/internal/waitlistsdb"
)

// The SQLStore's SignupStore: the queue, written on the request path and worked
// through by whoever is running the launch.
var _ SignupStore = (*SQLStore)(nil)

// Join adds somebody to a list.
//
// The list read, the suppression check and the insert share one transaction.
// Without it a signup could be written against a list that closed between the
// check and the write, or past a withdrawal that landed in the same instant —
// and the second of those is the obligation this package exists to keep.
//
// The suppression check is a read rather than a reliance on the unique index,
// for the reason settings' name check is: a constraint violation reaches a
// caller as a driver error naming an index, which they cannot tell apart from
// the database being unwell and cannot show to a person. The index is still what
// makes it true under a concurrent write.
func (s *SQLStore) Join(
	ctx context.Context,
	scope tenancy.Scope,
	listID string,
	signup *Signup,
) (*Signup, error) {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(listKey, listID),
	)
	defer op.End()

	var joined *Signup

	if err := s.client.WithTransaction(ctx, func(q database.Tx) (err error) {
		joined, err = s.join(ctx, op, q, scope, listID, signup)

		return err
	}); err != nil {
		return nil, op.Error(err, "joining waitlist %q", listID)
	}

	return joined, nil
}

// JoinTx is Join inside the caller's transaction, so the signup and whatever
// the caller writes beside it commit together or not at all.
//
// The list read and the suppression check run on q rather than on a transaction
// of the store's own, so a signup against a list opened earlier in the same
// transaction goes in, where Join would have to wait for a commit to see the
// list. See [Store].
func (s *SQLStore) JoinTx(
	ctx context.Context,
	q database.Tx,
	scope tenancy.Scope,
	listID string,
	signup *Signup,
) (*Signup, error) {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(listKey, listID),
	)
	defer op.End()

	if q == nil {
		return nil, op.Error(ErrNilExecutor, "joining waitlist %q", listID)
	}

	joined, err := s.join(ctx, op, q, scope, listID, signup)
	if err != nil {
		return nil, op.Error(err, "joining waitlist %q", listID)
	}

	return joined, nil
}

// join is the shared body of Join and JoinTx.
//
// It takes the executor rather than reaching for one, which is the whole of the
// difference between the two: every check, every statement, and the order they
// run in are the same on both paths, so neither can drift into accepting a
// signup the other refuses. The errors it returns are unwrapped; each caller
// reports them once, against its own span.
func (s *SQLStore) join(
	ctx context.Context,
	op observability.Operation,
	q waitlistsdb.DBTX,
	scope tenancy.Scope,
	listID string,
	signup *Signup,
) (*Signup, error) {
	if signup == nil {
		return nil, ErrNilSignup
	}

	if err := scope.Validate(); err != nil {
		return nil, err
	}

	if err := validateContact(signup.Contact); err != nil {
		return nil, err
	}

	if err := signup.Subject.Validate(); err != nil {
		return nil, err
	}

	// The status and the stamps are the store's. A caller that set them was
	// describing a signup that has already happened, and honoring it would put
	// somebody straight into a status the lifecycle guards exist to move them
	// through.
	joined := *signup
	joined.Scope = scope
	joined.ListID = listID
	joined.ContactDigest = s.Digest(signup.Contact)
	joined.Status = StatusWaiting
	joined.StatusChangedAt = nil
	joined.LastUpdatedAt = nil
	joined.ArchivedAt = nil

	if joined.ID == "" {
		joined.ID = identifiers.New()
	}

	op.Set(signupKey, joined.ID)

	list, err := s.readList(ctx, q, scope, listID)
	if err != nil {
		return nil, err
	}

	if !list.OpenAt(s.clock.Now()) {
		return nil, platformerrors.Wrapf(ErrListClosed, "waitlist %q closed at %s", listID, list.ClosesAt)
	}

	if err = s.refuseTakenContact(ctx, q, scope, listID, joined.ContactDigest); err != nil {
		return nil, err
	}

	if err = s.q.InsertSignup(ctx, q, insertSignupParams(&joined, scope)); err != nil {
		return nil, platformerrors.Wrap(err, "writing waitlist signup")
	}

	row, err := s.q.GetSignupCreatedAt(ctx, q, waitlistsdb.GetSignupCreatedAtParams{ID: joined.ID})
	if err != nil {
		return nil, platformerrors.Wrap(err, "reading back the waitlist signup's creation time")
	}

	joined.CreatedAt = row.CreatedAt.UTC()

	s.countSignups(ctx, StatusWaiting, 1)

	return &joined, nil
}

// GetSignup reads one live signup by id, on the list it belongs to.
func (s *SQLStore) GetSignup(
	ctx context.Context,
	scope tenancy.Scope,
	listID, signupID string,
) (*Signup, error) {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(listKey, listID),
		observability.WithValue(signupKey, signupID),
	)
	defer op.End()

	if err := scope.Validate(); err != nil {
		return nil, op.Error(err, "reading waitlist signup %q", signupID)
	}

	signup, err := s.readSignup(ctx, s.client.Reader(), scope, listID, signupID)
	if err != nil {
		return nil, op.Error(err, "reading waitlist signup %q", signupID)
	}

	return signup, nil
}

// GetSignupByContact reads one live signup by the address it was made with.
//
// The statement behind it sees archived rows, because it is the same statement
// Join's suppression check runs — see refuseTakenContact. An archived signup is
// not a live one, so this reports ErrSignupNotFound for it; a withdrawn signup
// is live and comes back, which is what lets an unsubscribe page say "you are
// already off this list" rather than "we have never heard of you".
func (s *SQLStore) GetSignupByContact(
	ctx context.Context,
	scope tenancy.Scope,
	listID, contact string,
) (*Signup, error) {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(listKey, listID),
	)
	defer op.End()

	if err := scope.Validate(); err != nil {
		return nil, op.Error(err, "reading waitlist signup by contact")
	}

	if err := validateContact(contact); err != nil {
		return nil, op.Error(err, "reading waitlist signup by contact")
	}

	signup, err := s.readSignupByDigest(ctx, s.client.Reader(), scope, listID, s.Digest(contact))
	if err != nil {
		return nil, op.Error(err, "reading waitlist signup by contact")
	}

	if signup.ArchivedAt != nil {
		return nil, op.Error(ErrSignupNotFound, "reading waitlist signup by contact")
	}

	return signup, nil
}

// ListSignups pages one list's live signups.
func (s *SQLStore) ListSignups(
	ctx context.Context,
	scope tenancy.Scope,
	listID string,
	filter *filtering.QueryFilter,
) (*filtering.QueryFilteredResult[Signup], error) {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(listKey, listID),
	)
	defer op.End()

	if err := scope.Validate(); err != nil {
		return nil, op.Error(err, "listing waitlist signups")
	}

	filter = pageFilter(filter)

	listRows, err := sortedRows(filter,
		func() ([]waitlistsdb.ListSignupsRow, error) {
			return s.q.ListSignups(ctx, s.client.Reader(), listSignupsParams(scope, listID, filter))
		},
		func() ([]waitlistsdb.ListSignupsDescendingRow, error) {
			return s.q.ListSignupsDescending(ctx, s.client.Reader(),
				waitlistsdb.ListSignupsDescendingParams(listSignupsParams(scope, listID, filter)))
		},
		func(r waitlistsdb.ListSignupsDescendingRow) waitlistsdb.ListSignupsRow {
			return waitlistsdb.ListSignupsRow(r)
		})
	if err != nil {
		return nil, op.Error(err, "listing waitlist signups")
	}

	rows := make([]pageRow[Signup], 0, len(listRows))
	for i := range listRows {
		rows = append(rows, signupPageRow(&listRows[i]))
	}

	op.SpanOnly(countKey, len(rows))

	return filtering.Drain(rows, pageValue, pageCounts,
		func(sg *Signup) string { return sg.ID }, filter), nil
}

// ListSignupsForSubject pages the signups belonging to one principal across
// every list in the scope, archived ones too where the filter asks for them.
func (s *SQLStore) ListSignupsForSubject(
	ctx context.Context,
	scope tenancy.Scope,
	subject Subject,
	filter *filtering.QueryFilter,
) (*filtering.QueryFilteredResult[Signup], error) {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(subjectKey, string(subject.Type)),
		observability.WithValue(subjectIDKey, subject.ID),
	)
	defer op.End()

	if err := scope.Validate(); err != nil {
		return nil, op.Error(err, "listing a subject's waitlist signups")
	}

	if err := requireSubject(subject); err != nil {
		return nil, op.Error(err, "listing a subject's waitlist signups")
	}

	filter = pageFilter(filter)

	listRows, err := sortedRows(filter,
		func() ([]waitlistsdb.ListSignupsForSubjectRow, error) {
			return s.q.ListSignupsForSubject(ctx, s.client.Reader(),
				listSignupsForSubjectParams(scope, subject, filter))
		},
		func() ([]waitlistsdb.ListSignupsForSubjectDescendingRow, error) {
			return s.q.ListSignupsForSubjectDescending(ctx, s.client.Reader(),
				waitlistsdb.ListSignupsForSubjectDescendingParams(
					listSignupsForSubjectParams(scope, subject, filter)))
		},
		func(r waitlistsdb.ListSignupsForSubjectDescendingRow) waitlistsdb.ListSignupsForSubjectRow {
			return waitlistsdb.ListSignupsForSubjectRow(r)
		})
	if err != nil {
		return nil, op.Error(err, "listing a subject's waitlist signups")
	}

	rows := make([]pageRow[Signup], 0, len(listRows))
	for i := range listRows {
		rows = append(rows, subjectSignupPageRow(&listRows[i]))
	}

	op.SpanOnly(countKey, len(rows))

	return filtering.Drain(rows, pageValue, pageCounts,
		func(sg *Signup) string { return sg.ID }, filter), nil
}

// UpdateSignupNotes rewrites the operator's note against a signup.
func (s *SQLStore) UpdateSignupNotes(
	ctx context.Context,
	scope tenancy.Scope,
	listID, signupID, notes string,
) error {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(listKey, listID),
		observability.WithValue(signupKey, signupID),
	)
	defer op.End()

	return op.Error(s.updateSignupNotes(ctx, s.client.Writer(), scope, listID, signupID, notes),
		"updating waitlist signup %q", signupID)
}

// UpdateSignupNotesTx is UpdateSignupNotes inside the caller's transaction. See
// [Store].
func (s *SQLStore) UpdateSignupNotesTx(
	ctx context.Context,
	q database.Tx,
	scope tenancy.Scope,
	listID, signupID, notes string,
) error {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(listKey, listID),
		observability.WithValue(signupKey, signupID),
	)
	defer op.End()

	if q == nil {
		return op.Error(ErrNilExecutor, "updating waitlist signup %q", signupID)
	}

	return op.Error(s.updateSignupNotes(ctx, q, scope, listID, signupID, notes),
		"updating waitlist signup %q", signupID)
}

// updateSignupNotes is the shared body of UpdateSignupNotes and
// UpdateSignupNotesTx, which differ in the executor they run on and in nothing
// else.
func (s *SQLStore) updateSignupNotes(
	ctx context.Context,
	q waitlistsdb.DBTX,
	scope tenancy.Scope,
	listID, signupID, notes string,
) error {
	if err := scope.Validate(); err != nil {
		return err
	}

	if err := requireID(signupID); err != nil {
		return err
	}

	count, err := s.q.UpdateSignupNotes(ctx, q, waitlistsdb.UpdateSignupNotesParams{
		Notes:      notes,
		ID:         signupID,
		Scope:      scope,
		WaitlistID: listID,
	})

	return guardCount(count, err, ErrSignupNotFound, "updating waitlist signup")
}

// Invite moves a waiting signup to invited.
func (s *SQLStore) Invite(ctx context.Context, scope tenancy.Scope, listID, signupID string) error {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(listKey, listID),
		observability.WithValue(signupKey, signupID),
		observability.WithValue(statusKey, string(StatusInvited)),
	)
	defer op.End()

	return op.Error(s.transition(ctx, s.client.Writer(), scope, listID, signupID, StatusWaiting, StatusInvited),
		"moving waitlist signup %q to %s", signupID, StatusInvited)
}

// InviteTx is Invite inside the caller's transaction, so the invitation and the
// record of who sent it land together or not at all. See [Store].
func (s *SQLStore) InviteTx(
	ctx context.Context,
	q database.Tx,
	scope tenancy.Scope,
	listID, signupID string,
) error {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(listKey, listID),
		observability.WithValue(signupKey, signupID),
		observability.WithValue(statusKey, string(StatusInvited)),
	)
	defer op.End()

	if q == nil {
		return op.Error(ErrNilExecutor, "moving waitlist signup %q to %s", signupID, StatusInvited)
	}

	return op.Error(s.transition(ctx, q, scope, listID, signupID, StatusWaiting, StatusInvited),
		"moving waitlist signup %q to %s", signupID, StatusInvited)
}

// Convert moves an invited signup to converted.
func (s *SQLStore) Convert(ctx context.Context, scope tenancy.Scope, listID, signupID string) error {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(listKey, listID),
		observability.WithValue(signupKey, signupID),
		observability.WithValue(statusKey, string(StatusConverted)),
	)
	defer op.End()

	return op.Error(s.transition(ctx, s.client.Writer(), scope, listID, signupID, StatusInvited, StatusConverted),
		"moving waitlist signup %q to %s", signupID, StatusConverted)
}

// ConvertTx is Convert inside the caller's transaction, which is the ordinary
// one: what somebody converted into is a row of the caller's, written in the
// same transaction. See [Store].
func (s *SQLStore) ConvertTx(
	ctx context.Context,
	q database.Tx,
	scope tenancy.Scope,
	listID, signupID string,
) error {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(listKey, listID),
		observability.WithValue(signupKey, signupID),
		observability.WithValue(statusKey, string(StatusConverted)),
	)
	defer op.End()

	if q == nil {
		return op.Error(ErrNilExecutor, "moving waitlist signup %q to %s", signupID, StatusConverted)
	}

	return op.Error(s.transition(ctx, q, scope, listID, signupID, StatusInvited, StatusConverted),
		"moving waitlist signup %q to %s", signupID, StatusConverted)
}

// transition is the guarded lifecycle move all four of Invite, InviteTx,
// Convert and ConvertTx are, on whatever executor the caller is holding.
//
// The guard is in the statement rather than in a read before it, which is what
// makes the move happen once: two requests inviting the same signup both find it
// waiting, and only one of their updates reports a row. Deciding on the read
// leaves a window as wide as whatever the caller does next, which for an
// invitation is an email.
//
// The refusal it reports is not certain about which of two things went wrong —
// the row is gone, or it is in another status — because a statement that matched
// nothing cannot tell them apart. So the guard's own answer is ErrWrongStatus
// and the ambiguity is resolved by one extra read, made only on the losing path,
// where a round trip costs nothing anybody is waiting on.
func (s *SQLStore) transition(
	ctx context.Context,
	q waitlistsdb.DBTX,
	scope tenancy.Scope,
	listID, signupID string,
	from, to Status,
) error {
	if err := scope.Validate(); err != nil {
		return err
	}

	if err := requireID(signupID); err != nil {
		return err
	}

	count, err := s.q.TransitionSignup(ctx, q,
		transitionSignupParams(scope, listID, signupID, from, to, s.clock.Now()))
	if err != nil {
		return platformerrors.Wrap(err, "moving waitlist signup")
	}

	if count == 0 {
		return s.explainLostTransition(ctx, q, scope, listID, signupID, from)
	}

	s.countSignups(ctx, to, 1)

	return nil
}

// Withdraw takes somebody off the list at their own request.
func (s *SQLStore) Withdraw(ctx context.Context, scope tenancy.Scope, listID, signupID string) error {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(listKey, listID),
		observability.WithValue(signupKey, signupID),
	)
	defer op.End()

	return op.Error(s.withdraw(ctx, s.client.Writer(), scope, listID, signupID),
		"withdrawing waitlist signup %q", signupID)
}

// WithdrawTx is Withdraw inside the caller's transaction. See [Store].
func (s *SQLStore) WithdrawTx(
	ctx context.Context,
	q database.Tx,
	scope tenancy.Scope,
	listID, signupID string,
) error {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(listKey, listID),
		observability.WithValue(signupKey, signupID),
	)
	defer op.End()

	if q == nil {
		return op.Error(ErrNilExecutor, "withdrawing waitlist signup %q", signupID)
	}

	return op.Error(s.withdraw(ctx, q, scope, listID, signupID),
		"withdrawing waitlist signup %q", signupID)
}

// withdraw is the shared body of Withdraw and WithdrawTx, which differ in the
// executor they run on and in nothing else.
func (s *SQLStore) withdraw(
	ctx context.Context,
	q waitlistsdb.DBTX,
	scope tenancy.Scope,
	listID, signupID string,
) error {
	if err := scope.Validate(); err != nil {
		return err
	}

	if err := requireID(signupID); err != nil {
		return err
	}

	count, err := s.q.WithdrawSignup(ctx, q, withdrawSignupParams(scope, listID, signupID, s.clock.Now()))
	if err != nil {
		return platformerrors.Wrap(err, "withdrawing waitlist signup")
	}

	if count == 0 {
		return s.explainLostTransition(ctx, q, scope, listID, signupID, StatusWithdrawn)
	}

	s.countSignups(ctx, StatusWithdrawn, 1)

	return nil
}

// WithdrawSignupsForSubject withdraws every signup one principal holds in the
// scope, archived signups included, and reports how many that was.
//
// It is one statement rather than a page of reads and a withdrawal per row, and
// the difference is what makes it an erasure. A read outside the caller's
// transaction sees the table as it was committed, so a signup landing between
// the read and the writes would survive; and the single-row withdrawal cannot
// reach an archived signup, which still holds the address it was made with. The
// statement keys on the subject and nothing else, so both rows are its.
//
// Zero is not an error. An erasure runs against whatever the subject actually
// left behind, and a person who never joined a list is a person with nothing
// here to erase — reporting that as a failure would fail an erasure that
// succeeded.
func (s *SQLStore) WithdrawSignupsForSubject(
	ctx context.Context,
	q database.Tx,
	scope tenancy.Scope,
	subject Subject,
) (int64, error) {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(subjectKey, string(subject.Type)),
		observability.WithValue(subjectIDKey, subject.ID),
	)
	defer op.End()

	if q == nil {
		return 0, op.Error(ErrNilExecutor, "erasing a subject's waitlist signups")
	}

	if err := scope.Validate(); err != nil {
		return 0, op.Error(err, "erasing a subject's waitlist signups")
	}

	if err := requireSubject(subject); err != nil {
		return 0, op.Error(err, "erasing a subject's waitlist signups")
	}

	withdrawn, err := s.q.WithdrawSignupsForSubject(ctx, q,
		withdrawSignupsForSubjectParams(scope, subject, s.clock.Now()))
	if err != nil {
		return 0, op.Error(platformerrors.Wrap(err, "withdrawing waitlist signups"),
			"erasing a subject's waitlist signups")
	}

	op.Set(countKey, withdrawn)

	if withdrawn > 0 {
		s.countSignups(ctx, StatusWithdrawn, withdrawn)
	}

	return withdrawn, nil
}

// ArchiveSignup retires a signup administratively.
func (s *SQLStore) ArchiveSignup(ctx context.Context, scope tenancy.Scope, listID, signupID string) error {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(listKey, listID),
		observability.WithValue(signupKey, signupID),
	)
	defer op.End()

	return op.Error(s.archiveSignup(ctx, s.client.Writer(), scope, listID, signupID),
		"archiving waitlist signup %q", signupID)
}

// ArchiveSignupTx is ArchiveSignup inside the caller's transaction. See
// [Store].
func (s *SQLStore) ArchiveSignupTx(
	ctx context.Context,
	q database.Tx,
	scope tenancy.Scope,
	listID, signupID string,
) error {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(listKey, listID),
		observability.WithValue(signupKey, signupID),
	)
	defer op.End()

	if q == nil {
		return op.Error(ErrNilExecutor, "archiving waitlist signup %q", signupID)
	}

	return op.Error(s.archiveSignup(ctx, q, scope, listID, signupID),
		"archiving waitlist signup %q", signupID)
}

// archiveSignup is the shared body of ArchiveSignup and ArchiveSignupTx, which
// differ in the executor they run on and in nothing else.
func (s *SQLStore) archiveSignup(
	ctx context.Context,
	q waitlistsdb.DBTX,
	scope tenancy.Scope,
	listID, signupID string,
) error {
	if err := scope.Validate(); err != nil {
		return err
	}

	if err := requireID(signupID); err != nil {
		return err
	}

	count, err := s.q.ArchiveSignup(ctx, q, waitlistsdb.ArchiveSignupParams{
		ID:         signupID,
		Scope:      scope,
		WaitlistID: listID,
	})

	return guardCount(count, err, ErrSignupNotFound, "archiving waitlist signup")
}

// explainLostTransition says why a guarded write matched nothing.
//
// It is one read on the losing path, and it exists because the count alone
// cannot tell "the signup is gone" from "the signup is somewhere else in its
// lifecycle", and those lead a caller to two different places: a broken link,
// or a person who has already been invited. The expected status is the one the
// write required — the one it required the row to hold, or, for a withdrawal, the one
// it required the row not to hold, which is why the withdrawn case is answered
// first.
//
// The read goes through the executor the write ran on. On the transactional
// path that is the only executor that can see a row this transaction wrote, and
// on the other it is the writer the statement just missed on, which cannot be
// behind it.
func (s *SQLStore) explainLostTransition(
	ctx context.Context,
	q waitlistsdb.DBTX,
	scope tenancy.Scope,
	listID, signupID string,
	expected Status,
) error {
	signup, err := s.readSignup(ctx, q, scope, listID, signupID)
	if err != nil {
		return err
	}

	if expected == StatusWithdrawn && signup.Withdrawn() {
		return ErrAlreadyWithdrawn
	}

	return platformerrors.Wrapf(ErrWrongStatus, "waitlist signup %q is %s", signupID, signup.Status)
}

// readSignup is the read by id, through whatever executor the caller is
// holding. It is the read GetSignup makes on the reader and the one a lost
// transition makes on the executor it lost on.
func (s *SQLStore) readSignup(
	ctx context.Context,
	q waitlistsdb.DBTX,
	scope tenancy.Scope,
	listID, signupID string,
) (*Signup, error) {
	if err := requireID(signupID); err != nil {
		return nil, err
	}

	row, err := s.q.GetSignup(ctx, q, waitlistsdb.GetSignupParams{
		ID:         signupID,
		Scope:      scope,
		WaitlistID: listID,
	})
	if err != nil {
		return nil, notFound(err, ErrSignupNotFound)
	}

	return signupFromRow(&row), nil
}

// refuseTakenContact reports why a contact cannot join this list, having found a
// row for its digest.
//
// The read sees archived rows, because the uniqueness it stands in for does —
// see waitlists/internal/queries. Which of the two refusals it is decides what
// the caller shows somebody: ErrContactWithdrawn is a person who asked to be
// left alone and must be, and ErrAlreadySignedUp is a person who is already in
// the queue.
func (s *SQLStore) refuseTakenContact(
	ctx context.Context,
	q waitlistsdb.DBTX,
	scope tenancy.Scope,
	listID, digest string,
) error {
	signup, err := s.readSignupByDigest(ctx, q, scope, listID, digest)

	switch {
	case errors.Is(err, ErrSignupNotFound):
		return nil
	case err != nil:
		return err
	case signup.Withdrawn():
		return ErrContactWithdrawn
	default:
		return ErrAlreadySignedUp
	}
}

// readSignupByDigest is the read keyed on a contact's digest, through whatever
// executor the caller is holding. It sees archived rows; the callers decide what
// one means.
func (s *SQLStore) readSignupByDigest(
	ctx context.Context,
	q waitlistsdb.DBTX,
	scope tenancy.Scope,
	listID, digest string,
) (*Signup, error) {
	row, err := s.q.GetSignupByContactDigest(ctx, q, waitlistsdb.GetSignupByContactDigestParams{
		Scope:         scope,
		WaitlistID:    listID,
		ContactDigest: digest,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrSignupNotFound
		}

		return nil, platformerrors.Wrap(err, "reading waitlist signup by contact digest")
	}

	return signupFromDigestRow(&row), nil
}

// requireSubject is what the two subject-keyed methods require of theirs: a
// whole subject, naming somebody.
//
// The anonymous subject is refused rather than answered. Both columns default
// to the empty string, so a statement bound to nobody would reach every signup
// nobody claimed — which for a read is the widest possible answer to a question
// about one person, and for the erasure is a withdrawal of everybody on every
// list who did not have an account.
func requireSubject(subject Subject) error {
	if subject.Anonymous() {
		return ErrEmptySubjectType
	}

	return subject.Validate()
}
