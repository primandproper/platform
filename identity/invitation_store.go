package identity

import (
	"context"
	"crypto/subtle"

	"github.com/primandproper/platform-go/v13/database"
	platformerrors "github.com/primandproper/platform-go/v13/errors"
	"github.com/primandproper/platform-go/v13/filtering"
	"github.com/primandproper/platform-go/v13/identity/internal/identitydb"
	"github.com/primandproper/platform-go/v13/identity/internal/queries"
	"github.com/primandproper/platform-go/v13/observability"
	"github.com/primandproper/platform-go/v13/tenancy"
)

// The SQLStore's InvitationStore: an invitation issued, looked up, and
// answered.
var _ InvitationStore = (*SQLStore)(nil)

// The invitation columns the two paged reads name, spelled where the canonical
// statements spell them, and the column the invitation role table keys on.
const (
	invitationFromUserColumn = queries.InvitationFromUserColumn
	invitationToEmailColumn  = queries.InvitationToEmailColumn
	invitationIDColumn       = "invitation_id"
)

// CreateInvitation writes an invitation and the roles it promises.
func (s *SQLStore) CreateInvitation(ctx context.Context, invitation *Invitation) error {
	ctx, op := s.o11y.Begin(ctx)
	defer op.End()

	if invitation == nil {
		return op.Error(ErrNilInvitation, "creating identity invitation")
	}

	invitation.EnsureDefaults()

	if err := invitation.ValidateWithContext(ctx); err != nil {
		return op.Error(err, "creating identity invitation")
	}

	if invitation.Status != InvitationPending {
		// An invitation created already answered has no flow that could have
		// answered it, and would sit in a terminal state nobody sent.
		return op.Error(
			platformerrors.Wrapf(ErrInvalidInvitationStatus, "status %q at creation", invitation.Status),
			"creating identity invitation",
		)
	}

	invitation.ID = newID(invitation.ID)

	op.Set(invitationIDKey, invitation.ID).
		Set(accountIDKey, invitation.BelongsToAccount).
		Set(scopeKey, invitation.Scope.String())

	// The invitation and its roles are one transaction: an invitation promising
	// no roles produces a membership that may do nothing, which is discovered
	// only once somebody has accepted it.
	if err := s.client.WithTransaction(ctx, func(q database.Tx) error {
		if err := s.q.CreateInvitation(ctx, q, createInvitationParams(invitation)); err != nil {
			return platformerrors.Wrap(err, "writing identity invitation")
		}

		// Read back for the reason CreateUser and CreateAccount read theirs
		// back — see stampCreatedAt.
		created, readErr := s.q.GetInvitationCreatedAt(ctx, q, identitydb.GetInvitationCreatedAtParams{
			ID: invitation.ID,
		})
		if err := stampCreatedAt(&invitation.CreatedAt, created.CreatedAt, readErr); err != nil {
			return err
		}

		return s.replaceRoles(ctx, q, s.tables.invitationRoles, invitationIDColumn, invitation.ID, invitation.Roles)
	}); err != nil {
		return op.Error(err, "creating identity invitation")
	}

	return nil
}

// GetInvitation reads one of the scope's invitations by ID.
func (s *SQLStore) GetInvitation(ctx context.Context, scope tenancy.Scope, invitationID string) (*Invitation, error) {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(invitationIDKey, invitationID),
	)
	defer op.End()

	if err := scope.Validate(); err != nil {
		return nil, op.Error(err, "reading identity invitation %q", invitationID)
	}

	invitation, err := s.readInvitation(ctx, s.client.Reader(), scope, invitationID)
	if err != nil {
		return nil, op.Error(err, "reading identity invitation %q", invitationID)
	}

	return invitation, nil
}

// readInvitation reads one invitation with its roles attached, through whatever
// executor the caller is holding.
func (s *SQLStore) readInvitation(
	ctx context.Context,
	q database.SQLQueryExecutor,
	scope tenancy.Scope,
	invitationID string,
) (*Invitation, error) {
	row, err := s.q.GetInvitation(ctx, q, identitydb.GetInvitationParams{ID: invitationID, Scope: scope})
	if err != nil {
		return nil, notFound(err, ErrInvitationNotFound)
	}

	invitation := invitationFromRow(&row)

	byInvitation, err := s.rolesFor(ctx, q, s.tables.invitationRoles, invitationIDColumn, []string{invitation.ID})
	if err != nil {
		return nil, err
	}

	invitation.Roles = byInvitation[invitation.ID]

	return invitation, nil
}

// GetInvitationByToken reads the invitation a link names, comparing the token.
func (s *SQLStore) GetInvitationByToken(ctx context.Context, scope tenancy.Scope, invitationID, token string) (*Invitation, error) {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(invitationIDKey, invitationID),
	)
	defer op.End()

	if err := scope.Validate(); err != nil {
		return nil, op.Error(err, "reading identity invitation by token")
	}

	invitation, err := s.readInvitation(ctx, s.client.Reader(), scope, invitationID)
	if err != nil {
		return nil, op.Error(err, "reading identity invitation by token")
	}

	if err = s.checkInvitationToken(invitation, token); err != nil {
		return nil, op.Error(err, "reading identity invitation by token")
	}

	return invitation, nil
}

// checkInvitationToken vets a presented token against a read invitation.
//
// The comparison is constant-time. It is comparing a secret the caller supplied
// against one from the database, and a byte-at-a-time compare leaks how much of
// a guess was right — which for a bearer credential that grants membership in
// somebody else's account is worth closing even though the ID has to be known
// first.
//
// A wrong token reads as not found rather than as forbidden, so the read is not
// an oracle for which invitation IDs exist. An expired one is distinguished,
// because the recipient can act on that: ask for another.
func (s *SQLStore) checkInvitationToken(invitation *Invitation, token string) error {
	if subtle.ConstantTimeCompare([]byte(invitation.Token), []byte(token)) != 1 {
		return ErrInvitationNotFound
	}

	if invitation.Status != InvitationPending {
		return ErrInvitationNotFound
	}

	if invitation.Expired(s.now()) {
		return platformerrors.Wrapf(ErrInvitationExpired, "invitation %q", invitation.ID)
	}

	return nil
}

// ListInvitationsFromUser pages the pending invitations a user has sent.
func (s *SQLStore) ListInvitationsFromUser(ctx context.Context, scope tenancy.Scope, userID string, filter *filtering.QueryFilter) (*filtering.QueryFilteredResult[Invitation], error) {
	return s.pageInvitations(ctx, invitationFromUserColumn, scope, userID, filter, "listing identity invitations from user")
}

// ListInvitationsForEmailAddress pages the pending invitations addressed to an
// email address.
func (s *SQLStore) ListInvitationsForEmailAddress(ctx context.Context, scope tenancy.Scope, emailAddress string, filter *filtering.QueryFilter) (*filtering.QueryFilteredResult[Invitation], error) {
	return s.pageInvitations(ctx, invitationToEmailColumn, scope, emailAddress, filter, "listing identity invitations for email address")
}

// pageInvitations is the one implementation behind both paged invitation reads.
// They differ in one column, and the parts that must not differ — the scope
// predicate, the pending clause, and the redaction — are written once here.
func (s *SQLStore) pageInvitations(
	ctx context.Context,
	column string,
	scope tenancy.Scope,
	value string,
	filter *filtering.QueryFilter,
	description string,
) (*filtering.QueryFilteredResult[Invitation], error) {
	ctx, op := s.o11y.Begin(ctx, observability.WithValue(scopeKey, scope.String()))
	defer op.End()

	if err := scope.Validate(); err != nil {
		return nil, op.Error(err, "%s", description)
	}

	filter = pageFilter(filter)

	rows, err := s.listInvitationRows(ctx, column, scope, value, filter)
	if err != nil {
		return nil, op.Error(err, "%s", description)
	}

	invitations := pageValues(rows)

	ids := make([]string, 0, len(invitations))
	for _, invitation := range invitations {
		ids = append(ids, invitation.ID)
	}

	byInvitation, err := s.rolesFor(ctx, s.client.Reader(), s.tables.invitationRoles, invitationIDColumn, ids)
	if err != nil {
		return nil, op.Error(err, "%s", description)
	}

	// Redacted, and the roles attached, in one pass, through the pointer the
	// page row holds. A listed invitation is rendered to whoever asked for the
	// list, and its token is the credential that accepts it — a sender's own
	// list would otherwise hand every recipient's link back to the sender's
	// browser.
	for _, invitation := range invitations {
		invitation.Roles = byInvitation[invitation.ID]
		*invitation = *invitation.Redacted()
	}

	op.SpanOnly(countKey, len(rows))

	return filtering.Drain(rows, pageValue, pageCounts,
		func(i *Invitation) string { return i.ID }, filter), nil
}

// listInvitationRows runs the generated list variant the column names, pending
// invitations only.
//
// The switch is closed on purpose: its two arms are the two canonical
// statements, and a third column is not a wider query — it is a statement that
// was never rendered or checked, which is exactly what the old map of rendered
// statements refused too. The status is bound rather than baked in, and both
// arms bind the same one, so "paged reads return only pending" stays a fact
// with one spelling.
func (s *SQLStore) listInvitationRows(
	ctx context.Context,
	column string,
	scope tenancy.Scope,
	value string,
	filter *filtering.QueryFilter,
) ([]pageRow[Invitation], error) {
	w := windowFrom(filter)

	switch column {
	case invitationFromUserColumn:
		got, err := s.q.ListInvitationsByFromUser(ctx, s.client.Reader(), identitydb.ListInvitationsByFromUserParams{
			CreatedAfter:    w.createdAfter,
			CreatedBefore:   w.createdBefore,
			UpdatedAfter:    w.updatedAfter,
			UpdatedBefore:   w.updatedBefore,
			IncludeArchived: w.includeArchived,
			Scope:           scope,
			FromUser:        value,
			Status:          InvitationPending.String(),
			PageCursor:      w.pageCursor,
			ResultLimit:     w.resultLimit,
		})
		if err != nil {
			return nil, err
		}

		rows := make([]pageRow[Invitation], 0, len(got))
		for i := range got {
			rows = append(rows, invitationPageRowFromUser(&got[i]))
		}

		return rows, nil

	case invitationToEmailColumn:
		got, err := s.q.ListInvitationsByToEmail(ctx, s.client.Reader(), identitydb.ListInvitationsByToEmailParams{
			CreatedAfter:    w.createdAfter,
			CreatedBefore:   w.createdBefore,
			UpdatedAfter:    w.updatedAfter,
			UpdatedBefore:   w.updatedBefore,
			IncludeArchived: w.includeArchived,
			Scope:           scope,
			ToEmail:         value,
			Status:          InvitationPending.String(),
			PageCursor:      w.pageCursor,
			ResultLimit:     w.resultLimit,
		})
		if err != nil {
			return nil, err
		}

		rows := make([]pageRow[Invitation], 0, len(got))
		for i := range got {
			rows = append(rows, invitationPageRowToEmail(&got[i]))
		}

		return rows, nil

	default:
		return nil, platformerrors.Newf("identity: no rendered invitation list keyed on %q", column)
	}
}

// AcceptInvitation marks an invitation accepted and writes the membership it
// promised, through the caller's transaction.
func (s *SQLStore) AcceptInvitation(
	ctx context.Context,
	q database.Tx,
	scope tenancy.Scope,
	invitationID, token, acceptingUserID, note string,
) (*Membership, error) {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(invitationIDKey, invitationID),
		observability.WithValue(userIDKey, acceptingUserID),
	)
	defer op.End()

	if err := requireExecutor(q); err != nil {
		return nil, op.Error(err, "accepting identity invitation")
	}

	if err := scope.Validate(); err != nil {
		return nil, op.Error(err, "accepting identity invitation")
	}

	if acceptingUserID == "" {
		return nil, op.Error(
			platformerrors.Wrap(platformerrors.ErrEmptyInputParameter, "no accepting user named"),
			"accepting identity invitation",
		)
	}

	invitation, err := s.readInvitation(ctx, q, scope, invitationID)
	if err != nil {
		return nil, op.Error(err, "accepting identity invitation")
	}

	if err = s.checkInvitationToken(invitation, token); err != nil {
		return nil, op.Error(err, "accepting identity invitation")
	}

	now := s.now()

	// The answer carries the same pending predicate every other answer does, so
	// two clicks on one link produce one membership: the second finds nothing
	// pending and stops here.
	query, args := s.tables.buildAnswerInvitation(
		s.dialect, scope, invitationID, InvitationAccepted, note, &acceptingUserID, now,
	)
	if err = s.execExpectingRow(ctx, op, q, query, args, ErrInvitationNotFound, "accepting identity invitation"); err != nil {
		return nil, op.Error(err, "accepting identity invitation")
	}

	membership := &Membership{
		Scope:            scope,
		BelongsToUser:    acceptingUserID,
		BelongsToAccount: invitation.BelongsToAccount,
		// The roles come off the invitation rather than from a parameter: what
		// somebody was invited to is what they get, and a parameter here is
		// where a privilege escalation goes in.
		Roles:     invitation.Roles,
		ID:        newID(""),
		CreatedAt: now,
	}

	existing, err := s.liveMembershipCount(ctx, q, scope, acceptingUserID)
	if err != nil {
		return nil, op.Error(err, "accepting identity invitation")
	}

	// Accepting into a first account makes that account the default, which is
	// what a registration-by-invitation relies on.
	if existing == 0 {
		membership.DefaultAccount = true
	}

	if err = s.writeMembership(ctx, q, membership); err != nil {
		return nil, op.Error(err, "accepting identity invitation")
	}

	return membership, nil
}

// SetInvitationStatus answers an invitation without producing a membership.
func (s *SQLStore) SetInvitationStatus(ctx context.Context, scope tenancy.Scope, invitationID string, status InvitationStatus, note string) error {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(invitationIDKey, invitationID),
	)
	defer op.End()

	if err := scope.Validate(); err != nil {
		return op.Error(err, "setting identity invitation status")
	}

	if !status.Valid() {
		return op.Error(
			platformerrors.Wrapf(platformerrors.ErrUnrecognizedInputValue, "invitation status %q", status),
			"setting identity invitation status",
		)
	}

	if status == InvitationAccepted || status == InvitationPending {
		// Accepting is AcceptInvitation, which writes the membership in the same
		// transaction; a status write here would leave an accepted invitation
		// that produced nothing. Returning to pending would revive a bearer
		// credential somebody already declined.
		return op.Error(
			platformerrors.Wrapf(ErrInvalidInvitationStatus, "status %q", status),
			"setting identity invitation status",
		)
	}

	query, args := s.tables.buildAnswerInvitation(s.dialect, scope, invitationID, status, note, nil, s.now())

	if err := s.execExpectingRow(ctx, op, s.client.Writer(), query, args, ErrInvitationNotFound, "setting identity invitation status"); err != nil {
		return op.Error(err, "setting identity invitation status")
	}

	return nil
}
