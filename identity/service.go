package identity

import (
	"context"

	"github.com/primandproper/platform-go/v14/database"
	platformerrors "github.com/primandproper/platform-go/v14/errors"
	"github.com/primandproper/platform-go/v14/observability"
	"github.com/primandproper/platform-go/v14/observability/logging"
	"github.com/primandproper/platform-go/v14/observability/metrics"
	"github.com/primandproper/platform-go/v14/observability/tracing"
	"github.com/primandproper/platform-go/v14/tenancy"
)

// serviceLayerName scopes the service's spans, logger, and instruments.
const serviceLayerName = serviceName + "_service"

// The names the service labels its instruments with, one per operation. They
// are constants rather than string literals at the call sites because a
// misspelled one is a second time series nobody notices until a dashboard is
// missing half its traffic.
const (
	opRegister                 = "register"
	opInvite                   = "invite"
	opAcceptInvitation         = "accept_invitation"
	opRejectInvitation         = "reject_invitation"
	opCancelInvitation         = "cancel_invitation"
	opTransferAccountOwnership = "transfer_account_ownership"
	opSetDefaultAccount        = "set_default_account"
	opArchiveUser              = "archive_user"
	opUpdateUserAccountStatus  = "update_user_account_status"
	opSetUserServiceRoles      = "set_user_service_roles"
)

// Registration is what a completed registration produced: the user, the account
// they own, and the membership that makes them a member of it.
//
// The User and Account are the caller's own values, written back — Register
// takes what the caller assembled and fills in the IDs and creation times the
// store generated. The Membership is minted by Register and is the user's
// default account, because it is the only one they hold.
type Registration struct {
	_ struct{} `json:"-"`

	// User is the registrant, as the caller supplied them. It still carries
	// whatever credentials they were assembled with — Register redacts nothing
	// the caller handed it.
	User *User `json:"user"`

	// Account is their first account, owned by them.
	Account *Account `json:"account"`

	// Membership puts the user in the account and is their default.
	Membership *Membership `json:"membership"`
}

// Acceptance is what accepting an invitation produced: the answered invitation,
// redacted, and the membership it promised.
type Acceptance struct {
	_ struct{} `json:"-"`

	// Invitation is the invitation as it stands after the answer — status
	// accepted, ToUser naming the acceptor. Its token is cleared.
	Invitation *Invitation `json:"invitation"`

	// Membership is what the acceptance minted, carrying the roles the
	// invitation promised.
	Membership *Membership `json:"membership"`
}

// Service is the orchestration over Store that every consumer of this package
// would otherwise write: the operations that are more than one write.
//
// Store owns the writes; this owns the ones that only mean something together.
// A registration is a user, an account and a membership, and a user who exists
// without an account signs in to nothing. An accepted invitation is a status
// and a membership. A transfer of ownership is two membership writes. Each of
// those is a transaction, and getting the transaction right — opening one,
// putting every write in it, letting the consumer's own writes join it — is
// what was being written again in every application that adopted the store.
// Measured in the consumer this package was extracted from, the layer this
// replaces is a little over two thousand lines.
//
// # What it is not
//
// It holds no policy, and the omissions are the design rather than an
// unfinished edge. Whether a registration needs a password, how long an
// invitation lives, who may invite, what a username may look like, which
// transactional email goes out — all of that is the consumer's, and this
// package's job is to give the answer a place to land. So Register takes a
// User the caller assembled and validated by their own rules; Invite takes an
// Invitation with the caller's expiry and token on it; and nothing here checks
// that the person calling is allowed to.
//
// It ships no transport either. There is no HTTP handler, no gRPC service and
// no proto here, for the reason the module README states under "Transports":
// what this module stores is not what a consumer serves.
//
// # The transaction, and where a consumer's writes go
//
// Each operation runs as one database.Tx, opened through the client's
// WithTransaction, and calls the matching Hooks method inside it. That is the
// seam a consumer's audit entry, data change event or search stamp goes
// through, and it is why the operations do not take a Tx of their own the way
// the store's writes do: an operation is several store writes plus the
// consumer's, and something has to own the transaction they share. Here that
// is the Service, and the hook is how the consumer gets into it.
//
// A hook returning an error rolls the whole operation back. See Hooks for what
// belongs in one and what does not.
//
// # What comes back
//
// Every entity the Service read back for itself is redacted before it is
// returned or handed to a hook: users lose their credentials, invitations lose
// their tokens. Entities the caller supplied are the caller's own values,
// mutated in place by the store and otherwise untouched — so a registration
// hands back the User it was given, hashed password and all, and Invite hands
// back the Invitation whose token the caller minted and still needs.
//
// The reads that produce those values run on the operation's own transaction,
// so what a hook and a caller see is what committed rather than what a second
// connection would have seen a moment later.
type Service struct {
	client database.Client
	store  Store
	hooks  Hooks
	o11y   observability.Observer

	instruments *metrics.OperationSet

	// What the options wrote, kept only until the observer is built from it.
	logger         logging.Logger
	tracerProvider tracing.Provider

	metricsProvider metrics.Provider
}

// NewService builds the orchestration layer over a Store.
//
// The client is kept, unlike the store's, because opening the transaction is
// what this layer is for: every operation runs inside Client.WithTransaction.
// The store is the seam the writes go through, and it is the interface rather
// than *SQLStore so that a consumer whose directory is not this schema still
// gets these operations.
//
// Hooks default to NoopHooks, so a consumer with nothing to commit alongside an
// identity write configures nothing. Observability is optional and defaults to
// nothing.
func NewService(client database.Client, store Store, opts ...ServiceOption) (*Service, error) {
	if client == nil {
		return nil, ErrNilDatabaseClient
	}

	if store == nil {
		return nil, ErrNilStore
	}

	s := &Service{
		client: client,
		store:  store,
		hooks:  NoopHooks{},
	}

	for _, opt := range opts {
		if opt != nil {
			opt(s)
		}
	}

	s.o11y = observability.NewObserver(serviceLayerName, s.logger, s.tracerProvider)

	instruments, err := metrics.NewOperationSet(s.metricsProvider, serviceLayerName)
	if err != nil {
		return nil, platformerrors.Wrap(err, "creating identity service instruments")
	}

	s.instruments = instruments

	return s, nil
}

// run is the shape every operation here has, in one place: the instruments, the
// transaction, and the error the transaction is aborted with.
//
// It exists because the three can be got wrong separately and silently. An
// operation that forgot to count an attempt leaves a latency histogram with no
// denominator; one that opened no transaction leaves a consumer's hook writing
// on a connection of its own; one that swallowed the callback's error commits a
// half-finished operation. Each operation below is then the writes it performs
// and nothing else.
func (s *Service) run(
	ctx context.Context,
	op observability.Operation,
	name string,
	fn func(tx database.Tx) error,
) (err error) {
	attr := operationAttr(name)

	s.instruments.Attempt(ctx, attr)

	defer op.Time(ctx, nil, s.instruments.Latency, attr)()

	defer func() {
		if err != nil {
			s.instruments.Failed(ctx, attr)
		}
	}()

	return s.client.WithTransaction(ctx, fn)
}

// Register creates a user, the first account they own, and the membership
// between them, in one transaction.
//
// The three are one operation because two of them alone are a broken state. A
// user with no account signs in to nothing; an account with no membership has
// an owner who is not on its roster, and every roster-driven permission check
// then refuses them. That is the failure every application discovers in
// production rather than in a test, and it is the whole reason this method
// exists rather than a doc comment showing the three calls.
//
// The account's owner is the registrant: an account naming nobody adopts them,
// and one naming somebody else is refused rather than corrected, which is the
// reading this package already takes of a scope that disagrees. The membership
// is minted here with ownerRoles, which are the consumer's role names and so
// are required — a membership with none is a member who may do nothing, and
// the Store refuses it for exactly that reason. It is the user's only
// membership and so becomes their default account.
//
// The caller's User and Account are written back: IDs, creation times and
// anything else the store stamps. Neither is redacted, because both are the
// caller's own values.
//
// Policy is the caller's, before the call: whether a password was required,
// whether an invitation had to be presented, what the account is named. What
// happens after the commit — the welcome email, the provisioning — belongs
// behind an outbox row Hooks.AfterRegister writes rather than in the hook
// itself.
func (s *Service) Register(
	ctx context.Context,
	scope tenancy.Scope,
	user *User,
	account *Account,
	ownerRoles []string,
) (*Registration, error) {
	ctx, op := s.o11y.Begin(ctx, observability.WithValue(scopeKey, scope.String()))
	defer op.End()

	if user == nil {
		return nil, op.Error(ErrNilUser, "registering identity user")
	}

	if account == nil {
		return nil, op.Error(ErrNilAccount, "registering identity account")
	}

	registration := &Registration{User: user, Account: account}

	err := s.run(ctx, op, opRegister, func(tx database.Tx) error {
		if err := s.store.CreateUser(ctx, tx, scope, user); err != nil {
			return err
		}

		op.Set(userIDKey, user.ID).Set(usernameKey, user.Username)

		// The registrant owns the account they registered with. An account
		// naming somebody else is a caller who assembled the wrong value, and
		// overwriting it would make "who owns this" answerable only by reading
		// what came back — the same objection ErrScopeMismatch answers for the
		// directory a write is for.
		switch account.OwnerUserID {
		case "", user.ID:
			account.OwnerUserID = user.ID
		default:
			return platformerrors.Wrapf(platformerrors.ErrUnrecognizedInputValue,
				"account names owner %q rather than the registering user", account.OwnerUserID)
		}

		if err := s.store.CreateAccount(ctx, tx, scope, account); err != nil {
			return err
		}

		op.Set(accountIDKey, account.ID)

		// DefaultAccount is stated rather than left to the store, which would
		// set it anyway for a user who holds nothing else. Stating it is what
		// makes this read as "their first account is where they land" instead
		// of relying on a rule enforced two layers down.
		membership := &Membership{
			Scope:            scope,
			BelongsToUser:    user.ID,
			BelongsToAccount: account.ID,
			Roles:            ownerRoles,
			DefaultAccount:   true,
		}

		if err := s.store.CreateMembership(ctx, tx, scope, membership); err != nil {
			return err
		}

		registration.Membership = membership

		return s.hooks.AfterRegister(ctx, tx, scope, registration)
	})
	if err != nil {
		return nil, op.Error(err, "registering identity user")
	}

	return registration, nil
}

// Invite issues an invitation, writing it and the roles it promises in one
// transaction with whatever Hooks.AfterInvite writes beside them.
//
// It is a single store write, and it is here anyway: the mail an invitation
// exists to send is the companion that must not be sent for an invitation that
// did not commit. A consumer queues it from the hook, on the transaction, and
// the queue row and the invitation land together or neither does.
//
// The invitation is the caller's — its expiry, its token, its roles, its note.
// Nothing here decides how long a link lives or what it may grant.
func (s *Service) Invite(ctx context.Context, scope tenancy.Scope, invitation *Invitation) error {
	ctx, op := s.o11y.Begin(ctx, observability.WithValue(scopeKey, scope.String()))
	defer op.End()

	if invitation == nil {
		return op.Error(ErrNilInvitation, "issuing identity invitation")
	}

	err := s.run(ctx, op, opInvite, func(tx database.Tx) error {
		if err := s.store.CreateInvitation(ctx, tx, scope, invitation); err != nil {
			return err
		}

		op.Set(invitationIDKey, invitation.ID).Set(accountIDKey, invitation.BelongsToAccount)

		return s.hooks.AfterInvite(ctx, tx, scope, invitation)
	})
	if err != nil {
		return op.Error(err, "issuing identity invitation")
	}

	return nil
}

// AcceptInvitation answers an invitation and mints the membership it promised,
// in one transaction.
//
// The two are one operation for the reason InvitationStore.AcceptInvitation
// gives: an accepted invitation without a membership is somebody who was told
// they joined and did not. The membership becomes the acceptor's default when
// it is the first they hold anywhere, which is what a registration by
// invitation relies on.
//
// The token is checked by the store against the invitation the ID names, and an
// expired one comes back as ErrInvitationExpired rather than
// ErrInvitationNotFound so the recipient can be told to ask for another. Two
// clicks on one link produce one membership: the second finds nothing pending.
//
// statusNote is the acceptor's, and lands beside the sender's untouched note.
func (s *Service) AcceptInvitation(
	ctx context.Context,
	scope tenancy.Scope,
	invitationID, token, acceptingUserID, statusNote string,
) (*Acceptance, error) {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(invitationIDKey, invitationID),
		observability.WithValue(userIDKey, acceptingUserID),
	)
	defer op.End()

	acceptance := &Acceptance{}

	err := s.run(ctx, op, opAcceptInvitation, func(tx database.Tx) error {
		membership, err := s.store.AcceptInvitation(ctx, tx, scope, invitationID, token, acceptingUserID, statusNote)
		if err != nil {
			return err
		}

		// Read on the transaction that just answered it, so the invitation the
		// hook records is the one that committed rather than the pending row a
		// second connection would still be seeing.
		invitation, err := s.store.GetInvitation(ctx, tx, scope, invitationID)
		if err != nil {
			return err
		}

		acceptance.Invitation, acceptance.Membership = invitation.Redacted(), membership

		return s.hooks.AfterAcceptInvitation(ctx, tx, scope, acceptance)
	})
	if err != nil {
		return nil, op.Error(err, "accepting identity invitation %q", invitationID)
	}

	return acceptance, nil
}

// RejectInvitation declines an invitation on the recipient's behalf.
//
// It takes the token and the store checks it, which is the difference between
// this and InvitationStore.SetInvitationStatus: that write is addressed by ID
// alone, so anybody holding an invitation's ID could answer it. A rejection
// arrives from whoever followed the link, and the link is the token.
//
// The invitation comes back redacted, as it stood after the answer.
func (s *Service) RejectInvitation(
	ctx context.Context,
	scope tenancy.Scope,
	invitationID, token, statusNote string,
) (*Invitation, error) {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(invitationIDKey, invitationID),
	)
	defer op.End()

	var answered *Invitation

	err := s.run(ctx, op, opRejectInvitation, func(tx database.Tx) error {
		// The token check, and the only reason this read is here: it reports
		// ErrInvitationExpired and ErrInvitationNotFound for a link that cannot
		// be answered, before a status write addressed by ID alone would have
		// answered it anyway.
		if _, err := s.store.GetInvitationByToken(ctx, tx, scope, invitationID, token); err != nil {
			return err
		}

		if err := s.store.SetInvitationStatus(
			ctx, tx, scope, invitationID, InvitationRejected, statusNote,
		); err != nil {
			return err
		}

		invitation, err := s.store.GetInvitation(ctx, tx, scope, invitationID)
		if err != nil {
			return err
		}

		answered = invitation.Redacted()

		return s.hooks.AfterRejectInvitation(ctx, tx, scope, answered)
	})
	if err != nil {
		return nil, op.Error(err, "rejecting identity invitation %q", invitationID)
	}

	return answered, nil
}

// CancelInvitation withdraws an invitation on the sender's behalf.
//
// No token, because the sender never had one: they are looking at what they
// sent, addressed by ID. Whether this caller is the sender is the consumer's
// check — Invitation.FromUser is what it resolves against — for the reason
// nothing else here decides who may act.
//
// An invitation that has already been answered is ErrInvitationNotFound: the
// status write matches only a pending row, which is what makes a cancellation
// that raced an acceptance leave the acceptance standing.
func (s *Service) CancelInvitation(
	ctx context.Context,
	scope tenancy.Scope,
	invitationID, statusNote string,
) (*Invitation, error) {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(invitationIDKey, invitationID),
	)
	defer op.End()

	var answered *Invitation

	err := s.run(ctx, op, opCancelInvitation, func(tx database.Tx) error {
		if err := s.store.SetInvitationStatus(
			ctx, tx, scope, invitationID, InvitationCancelled, statusNote,
		); err != nil {
			return err
		}

		invitation, err := s.store.GetInvitation(ctx, tx, scope, invitationID)
		if err != nil {
			return err
		}

		answered = invitation.Redacted()

		return s.hooks.AfterCancelInvitation(ctx, tx, scope, answered)
	})
	if err != nil {
		return nil, op.Error(err, "cancelling identity invitation %q", invitationID)
	}

	return answered, nil
}

// TransferAccountOwnership moves an account to a new owner.
//
// The store's write is already two membership writes and an account update that
// must commit together; what this adds is the previous owner, read before the
// column moves, and the hook that records the move. A transfer nobody can say
// the origin of is not a transfer anybody can audit.
//
// The account comes back as it stands afterwards. Transferring to the owner an
// account already has is a no-op that still runs the hook, naming the same user
// on both sides — the honest report of what was asked for.
func (s *Service) TransferAccountOwnership(
	ctx context.Context,
	scope tenancy.Scope,
	accountID, newOwnerUserID string,
) (*Account, error) {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(accountIDKey, accountID),
		observability.WithValue(userIDKey, newOwnerUserID),
	)
	defer op.End()

	var transferred *Account

	err := s.run(ctx, op, opTransferAccountOwnership, func(tx database.Tx) error {
		before, err := s.store.GetAccount(ctx, tx, scope, accountID)
		if err != nil {
			return err
		}

		previousOwnerUserID := before.OwnerUserID

		if err = s.store.TransferAccountOwnership(ctx, tx, scope, accountID, newOwnerUserID); err != nil {
			return err
		}

		if transferred, err = s.store.GetAccount(ctx, tx, scope, accountID); err != nil {
			return err
		}

		return s.hooks.AfterTransferAccountOwnership(ctx, tx, scope, transferred, previousOwnerUserID)
	})
	if err != nil {
		return nil, op.Error(err, "transferring ownership of identity account %q", accountID)
	}

	return transferred, nil
}

// SetDefaultAccount marks one of a user's accounts as the one they land in.
//
// The store clears the flag from the others in the same statement pair, so the
// invariant is one default per user rather than one per call. What this adds is
// the account that held it before, which the membership read finds on the way
// to checking that the user is a member of the one being named.
//
// A user who is not a live member of the account is ErrMembershipNotFound.
func (s *Service) SetDefaultAccount(
	ctx context.Context,
	scope tenancy.Scope,
	userID, accountID string,
) (*Membership, error) {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(userIDKey, userID),
		observability.WithValue(accountIDKey, accountID),
	)
	defer op.End()

	var current *Membership

	err := s.run(ctx, op, opSetDefaultAccount, func(tx database.Tx) error {
		// One read for two facts: which account the user lands in today, and
		// the memberships this one has to be among. Reading the target on its
		// own would answer the second and leave the hook unable to say what
		// changed.
		memberships, err := s.store.ListMembershipsForUser(ctx, tx, scope, userID)
		if err != nil {
			return err
		}

		var previousAccountID string

		for _, membership := range memberships {
			if membership.DefaultAccount {
				previousAccountID = membership.BelongsToAccount

				break
			}
		}

		if err = s.store.SetDefaultAccount(ctx, tx, scope, userID, accountID); err != nil {
			return err
		}

		if current, err = s.store.GetMembership(ctx, tx, scope, userID, accountID); err != nil {
			return err
		}

		return s.hooks.AfterSetDefaultAccount(ctx, tx, scope, current, previousAccountID)
	})
	if err != nil {
		return nil, op.Error(err, "setting default identity account for user %q", userID)
	}

	return current, nil
}

// ArchiveUser soft-deletes a user, ends every membership they hold, and hands
// the hook both — the user as they were, and the accounts they were on.
//
// The two reads are before the archival because they cannot be after it: an
// archived user is returned by no read here, and the memberships are archived
// with them. A consumer keeping rosters, search documents or per-account
// derived state of its own needs the list of accounts the subject just left,
// and this is the last moment anything can produce it.
//
// Archiving a user who still owns a live account is refused with
// ErrLastAccountOwner, naming the account: an ownerless account fails every
// permission check that resolves through its owner. Transfer it first — which
// is TransferAccountOwnership, one call away and hooked the same way.
//
// The user handed back and passed to the hook is redacted.
func (s *Service) ArchiveUser(ctx context.Context, scope tenancy.Scope, userID string) (*User, error) {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(userIDKey, userID),
	)
	defer op.End()

	var archived *User

	err := s.run(ctx, op, opArchiveUser, func(tx database.Tx) error {
		user, err := s.store.GetUser(ctx, tx, scope, userID)
		if err != nil {
			return err
		}

		memberships, err := s.store.ListMembershipsForUser(ctx, tx, scope, userID)
		if err != nil {
			return err
		}

		if err = s.store.ArchiveUser(ctx, tx, scope, userID); err != nil {
			return err
		}

		archived = user.Redacted()

		return s.hooks.AfterArchiveUser(ctx, tx, scope, archived, memberships)
	})
	if err != nil {
		return nil, op.Error(err, "archiving identity user %q", userID)
	}

	return archived, nil
}

// UpdateUserAccountStatus moves a user between statuses and reports what they
// held before.
//
// A ban, a termination, a reinstatement. The previous status is read on the way
// to confirming the user exists, so the hook that records the change can record
// it as a change rather than as a state.
//
// The user handed back and passed to the hook is read after the write, on the
// transaction that made it, and is redacted.
func (s *Service) UpdateUserAccountStatus(
	ctx context.Context,
	scope tenancy.Scope,
	userID string,
	status AccountStatus,
	explanation string,
) (*User, error) {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(userIDKey, userID),
	)
	defer op.End()

	var updated *User

	err := s.run(ctx, op, opUpdateUserAccountStatus, func(tx database.Tx) error {
		before, err := s.store.GetUser(ctx, tx, scope, userID)
		if err != nil {
			return err
		}

		previousStatus := before.AccountStatus

		if err = s.store.UpdateUserAccountStatus(ctx, tx, scope, userID, status, explanation); err != nil {
			return err
		}

		after, err := s.store.GetUser(ctx, tx, scope, userID)
		if err != nil {
			return err
		}

		updated = after.Redacted()

		return s.hooks.AfterUpdateUserAccountStatus(ctx, tx, scope, updated, previousStatus)
	})
	if err != nil {
		return nil, op.Error(err, "updating account status of identity user %q", userID)
	}

	return updated, nil
}

// SetUserServiceRoles replaces the roles a user holds outside any account and
// reports the set they held before.
//
// This is the write that grants and withdraws operator access, and it replaces
// rather than merges — a merging setter cannot revoke. Both sets reach the
// hook, because a record saying somebody holds a role is not the record an
// investigation wants; the one it wants says they gained it, and when.
//
// The user handed back and passed to the hook is read after the write, on the
// transaction that made it, and is redacted.
func (s *Service) SetUserServiceRoles(
	ctx context.Context,
	scope tenancy.Scope,
	userID string,
	roles []string,
) (*User, error) {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(userIDKey, userID),
	)
	defer op.End()

	var updated *User

	err := s.run(ctx, op, opSetUserServiceRoles, func(tx database.Tx) error {
		before, err := s.store.GetUser(ctx, tx, scope, userID)
		if err != nil {
			return err
		}

		previousRoles := before.ServiceRoles

		if err = s.store.SetUserServiceRoles(ctx, tx, scope, userID, roles); err != nil {
			return err
		}

		after, err := s.store.GetUser(ctx, tx, scope, userID)
		if err != nil {
			return err
		}

		updated = after.Redacted()

		return s.hooks.AfterSetUserServiceRoles(ctx, tx, scope, updated, previousRoles)
	})
	if err != nil {
		return nil, op.Error(err, "setting service roles of identity user %q", userID)
	}

	return updated, nil
}
