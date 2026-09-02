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

	if signup == nil {
		return nil, op.Error(ErrNilSignup, "joining waitlist")
	}

	if err := scope.Validate(); err != nil {
		return nil, op.Error(err, "joining waitlist %q", listID)
	}

	if err := validateContact(signup.Contact); err != nil {
		return nil, op.Error(err, "joining waitlist %q", listID)
	}

	if err := signup.Subject.Validate(); err != nil {
		return nil, op.Error(err, "joining waitlist %q", listID)
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

	if err := s.client.WithTransaction(ctx, func(q database.Tx) error {
		list, err := s.readList(ctx, q, scope, listID)
		if err != nil {
			return err
		}

		if !list.OpenAt(s.clock.Now()) {
			return platformerrors.Wrapf(ErrListClosed, "waitlist %q closed at %s", listID, list.ClosesAt)
		}

		if err = s.refuseTakenContact(ctx, q, scope, listID, joined.ContactDigest); err != nil {
			return err
		}

		if err = s.q.InsertSignup(ctx, q, insertSignupParams(&joined, scope)); err != nil {
			return platformerrors.Wrap(err, "writing waitlist signup")
		}

		row, err := s.q.GetSignupCreatedAt(ctx, q, waitlistsdb.GetSignupCreatedAtParams{ID: joined.ID})
		if err != nil {
			return platformerrors.Wrap(err, "reading back the waitlist signup's creation time")
		}

		joined.CreatedAt = row.CreatedAt.UTC()

		return nil
	}); err != nil {
		return nil, op.Error(err, "joining waitlist %q", listID)
	}

	s.countSignup(ctx, StatusWaiting)

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

	if err := requireID(signupID); err != nil {
		return nil, op.Error(err, "reading waitlist signup %q", signupID)
	}

	row, err := s.q.GetSignup(ctx, s.client.Reader(), waitlistsdb.GetSignupParams{
		ID:         signupID,
		Scope:      scope,
		WaitlistID: listID,
	})
	if err != nil {
		return nil, op.Error(notFound(err, ErrSignupNotFound), "reading waitlist signup %q", signupID)
	}

	return signupFromRow(&row), nil
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
// every list in the scope.
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

	// The anonymous subject is refused rather than answered. Both columns
	// default to the empty string, so a read bound to nobody would page every
	// signup nobody claimed — which is the widest possible answer to a question
	// about one person.
	if subject.Anonymous() {
		return nil, op.Error(ErrEmptySubjectType, "listing a subject's waitlist signups")
	}

	if err := subject.Validate(); err != nil {
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

	if err := scope.Validate(); err != nil {
		return op.Error(err, "updating waitlist signup %q", signupID)
	}

	if err := requireID(signupID); err != nil {
		return op.Error(err, "updating waitlist signup %q", signupID)
	}

	count, err := s.q.UpdateSignupNotes(ctx, s.client.Writer(), waitlistsdb.UpdateSignupNotesParams{
		Notes:      notes,
		ID:         signupID,
		Scope:      scope,
		WaitlistID: listID,
	})
	if err = guardCount(count, err, ErrSignupNotFound, "updating waitlist signup"); err != nil {
		return op.Error(err, "updating waitlist signup %q", signupID)
	}

	return nil
}

// Invite moves a waiting signup to invited.
func (s *SQLStore) Invite(ctx context.Context, scope tenancy.Scope, listID, signupID string) error {
	return s.transition(ctx, scope, listID, signupID, StatusWaiting, StatusInvited)
}

// Convert moves an invited signup to converted.
func (s *SQLStore) Convert(ctx context.Context, scope tenancy.Scope, listID, signupID string) error {
	return s.transition(ctx, scope, listID, signupID, StatusInvited, StatusConverted)
}

// transition is the guarded lifecycle move both Invite and Convert are.
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
	scope tenancy.Scope,
	listID, signupID string,
	from, to Status,
) error {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(listKey, listID),
		observability.WithValue(signupKey, signupID),
		observability.WithValue(statusKey, string(to)),
	)
	defer op.End()

	if err := scope.Validate(); err != nil {
		return op.Error(err, "moving waitlist signup %q to %s", signupID, to)
	}

	if err := requireID(signupID); err != nil {
		return op.Error(err, "moving waitlist signup %q to %s", signupID, to)
	}

	count, err := s.q.TransitionSignup(ctx, s.client.Writer(),
		transitionSignupParams(scope, listID, signupID, from, to, s.clock.Now()))
	if err != nil {
		return op.Error(platformerrors.Wrap(err, "moving waitlist signup"),
			"moving waitlist signup %q to %s", signupID, to)
	}

	if count == 0 {
		return op.Error(s.explainLostTransition(ctx, scope, listID, signupID, from),
			"moving waitlist signup %q to %s", signupID, to)
	}

	s.countSignup(ctx, to)

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

	if err := scope.Validate(); err != nil {
		return op.Error(err, "withdrawing waitlist signup %q", signupID)
	}

	if err := requireID(signupID); err != nil {
		return op.Error(err, "withdrawing waitlist signup %q", signupID)
	}

	count, err := s.q.WithdrawSignup(ctx, s.client.Writer(),
		withdrawSignupParams(scope, listID, signupID, s.clock.Now()))
	if err != nil {
		return op.Error(platformerrors.Wrap(err, "withdrawing waitlist signup"),
			"withdrawing waitlist signup %q", signupID)
	}

	if count == 0 {
		return op.Error(s.explainLostTransition(ctx, scope, listID, signupID, StatusWithdrawn),
			"withdrawing waitlist signup %q", signupID)
	}

	s.countSignup(ctx, StatusWithdrawn)

	return nil
}

// ArchiveSignup retires a signup administratively.
func (s *SQLStore) ArchiveSignup(ctx context.Context, scope tenancy.Scope, listID, signupID string) error {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(listKey, listID),
		observability.WithValue(signupKey, signupID),
	)
	defer op.End()

	if err := scope.Validate(); err != nil {
		return op.Error(err, "archiving waitlist signup %q", signupID)
	}

	if err := requireID(signupID); err != nil {
		return op.Error(err, "archiving waitlist signup %q", signupID)
	}

	count, err := s.q.ArchiveSignup(ctx, s.client.Writer(), waitlistsdb.ArchiveSignupParams{
		ID:         signupID,
		Scope:      scope,
		WaitlistID: listID,
	})
	if err = guardCount(count, err, ErrSignupNotFound, "archiving waitlist signup"); err != nil {
		return op.Error(err, "archiving waitlist signup %q", signupID)
	}

	return nil
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
func (s *SQLStore) explainLostTransition(
	ctx context.Context,
	scope tenancy.Scope,
	listID, signupID string,
	expected Status,
) error {
	signup, err := s.GetSignup(ctx, scope, listID, signupID)
	if err != nil {
		return err
	}

	if expected == StatusWithdrawn && signup.Withdrawn() {
		return ErrAlreadyWithdrawn
	}

	return platformerrors.Wrapf(ErrWrongStatus, "waitlist signup %q is %s", signupID, signup.Status)
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
	q database.SQLQueryExecutor,
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
	q database.SQLQueryExecutor,
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
