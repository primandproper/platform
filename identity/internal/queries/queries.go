package queries

import (
	"github.com/primandproper/platform-go/v13/database/dialect"
	"github.com/primandproper/platform-go/v13/database/querygen"
)

// The tables this package owns, at their canonical spelling — what the emitted
// .sql names, and what identity's own prefix rendering starts from.
const (
	UsersTable           = "identity_users"
	UserRolesTable       = "identity_user_roles"
	AccountsTable        = "identity_accounts"
	MembershipsTable     = "identity_memberships"
	MembershipRolesTable = "identity_membership_roles"
	InvitationsTable     = "identity_invitations"
	InvitationRolesTable = "identity_invitation_roles"
)

// ScopeColumn is the tenancy dimension every table here carries and every
// statement is keyed on. It is a column, not a convention: an unscoped read of
// this schema is not expressible, because there is no statement that omits it.
const ScopeColumn = "scope"

// The invitation columns the keyed list variants below name. Exported because
// the store spells them too — its argument maps key on them — and two spellings
// of one column is the drift the rest of this package exists to prevent.
const (
	InvitationFromUserColumn = "from_user"
	InvitationToEmailColumn  = "to_email"
	InvitationStatusColumn   = "status"
)

// The columns the field-specific writes assign, and the arguments the guarded
// ones compare against.
//
// A guard's argument cannot be spelled with the column it names. Requiring the
// invitation to still be pending while setting its status is one comparison
// against the stored value and one assignment of the new one, and under a single
// argument name the statement would set the column to the value it had just
// required it to already hold — legal SQL that guards nothing. So the guards are
// named apart, and the "current_" prefix says which end of the comparison each
// one is.
const (
	hashedPasswordColumn           = "hashed_password"
	requiresPasswordChangeColumn   = "requires_password_change"
	passwordLastChangedAtColumn    = "password_last_changed_at"
	twoFactorColumn                = "two_factor_secret"
	twoFactorVerifiedAtColumn      = "two_factor_secret_verified_at"
	accountStatusColumn            = "account_status"
	accountStatusExplanationColumn = "account_status_explanation"
	emailVerificationTokenColumn   = "email_address_verification_token"
	ownerUserIDColumn              = "owner_user_id"
	invitationNoteColumn           = "note"
	invitationToUserColumn         = "to_user"

	currentEmailVerificationTokenArg = "current_" + emailVerificationTokenColumn
	currentOwnerUserIDArg            = "current_" + ownerUserIDColumn
	currentInvitationStatusArg       = "current_" + InvitationStatusColumn
)

// EmailAddressVerifiedAtColumn is the proof a user's address is reachable.
//
// Spelled once because three declarations below name it — it is nullable, it is
// updatable, and it is in the projection — and because the store reasons about
// its value rather than only assigning it: moving an address clears it.
const EmailAddressVerifiedAtColumn = "email_address_verified_at"

// Users is the directory's people.
//
// Everything after last_name is either a credential, a proof, or a status, and
// each is written by the method that owns it — which is why the standard update
// assigns four profile columns and one derived fifth. email_address_verified_at
// is updatable on purpose: moving an address has to clear the proof that went
// with it, or a user could take an address they have never proven and stay
// verified.
var Users = Table{
	Name:     UsersTable,
	Singular: "User",
	Plural:   "Users",
	Columns: []string{
		querygen.IDColumn,
		ScopeColumn,
		"username",
		"email_address",
		"first_name",
		"last_name",
		"hashed_password",
		"requires_password_change",
		"password_last_changed_at",
		"two_factor_secret",
		"two_factor_secret_verified_at",
		EmailAddressVerifiedAtColumn,
		"email_address_verification_token",
		"account_status",
		"account_status_explanation",
		"last_accepted_terms_of_service",
		"last_accepted_privacy_policy",
		querygen.CreatedAtColumn,
		querygen.LastUpdatedAtColumn,
		querygen.ArchivedAtColumn,
	},
	Nullable: []string{
		"password_last_changed_at",
		"two_factor_secret_verified_at",
		EmailAddressVerifiedAtColumn,
		"last_accepted_terms_of_service",
		"last_accepted_privacy_policy",
	},
	Updatable: []string{
		"username",
		"email_address",
		"first_name",
		"last_name",
		EmailAddressVerifiedAtColumn,
	},
	Omitted: []querygen.StandardQuery{querygen.ExistsQuery},
}

// Accounts is what users belong to and what invoices are addressed to.
//
// The owner and every billing column are immutable to the standard update:
// transferring ownership names the current owner in its predicate so two
// concurrent transfers cannot both win, and a processor webhook carrying one
// field must not read-modify-write the rest.
var Accounts = Table{
	Name:     AccountsTable,
	Singular: "Account",
	Plural:   "Accounts",
	Columns: []string{
		querygen.IDColumn,
		ScopeColumn,
		"name",
		"owner_user_id",
		"billing_status",
		"subscription_plan_id",
		"payment_processor_customer_id",
		"last_payment_provider_synced_at",
		"address_line1",
		"address_line2",
		"address_city",
		"address_state",
		"address_postal_code",
		"address_country",
		"address_phone",
		"time_zone",
		querygen.CreatedAtColumn,
		querygen.LastUpdatedAtColumn,
		querygen.ArchivedAtColumn,
	},
	Nullable: []string{
		"subscription_plan_id",
		"last_payment_provider_synced_at",
	},
	Updatable: []string{
		"name",
		"address_line1",
		"address_line2",
		"address_city",
		"address_state",
		"address_postal_code",
		"address_country",
		"address_phone",
		"time_zone",
	},
	Omitted: []querygen.StandardQuery{querygen.ExistsQuery},
}

// Invitations is an offer of membership addressed to an email address.
//
// It gets no standard update and no archive. An invitation is answered rather
// than edited, and the answer is a status-guarded write — still pending in the
// predicate, so two clicks on one link produce one membership — which is
// AnswerInvitation in fieldWrites rather than the whole-row assignment the
// standard set would emit.
var Invitations = Table{
	Name:     InvitationsTable,
	Singular: "Invitation",
	Plural:   "Invitations",
	Columns: []string{
		querygen.IDColumn,
		ScopeColumn,
		"belongs_to_account",
		"from_user",
		"to_email",
		"to_name",
		"to_user",
		"token",
		"status",
		"note",
		"expires_at",
		querygen.CreatedAtColumn,
		querygen.LastUpdatedAtColumn,
		querygen.ArchivedAtColumn,
	},
	Nullable: []string{"to_user"},
	Omitted: []querygen.StandardQuery{
		querygen.ExistsQuery,
		querygen.UpdateQuery,
		querygen.ArchiveQuery,
	},
}

// Memberships is the many-to-many between the two, with facts of its own.
//
// It is declared for its columns and emits nothing — see the package comment.
var Memberships = Table{
	Name:     MembershipsTable,
	Singular: "Membership",
	Plural:   "Memberships",
	Columns: []string{
		querygen.IDColumn,
		ScopeColumn,
		"belongs_to_user",
		"belongs_to_account",
		"default_account",
		querygen.CreatedAtColumn,
		querygen.LastUpdatedAtColumn,
		querygen.ArchivedAtColumn,
	},
}

// Emitted is the tables the canonical .sql covers, in the order they appear in
// it.
var Emitted = []*Table{&Users, &Accounts, &Invitations}

// Render returns the canonical sqlc input for d: every emitted table's standard
// queries, in one file's worth of text.
//
// It is what identity/internal/queriesgen writes to the .sql beside this file
// and what CI regenerates to check the committed copy still matches. That .sql
// is sqlc-gen-unison's input, so what the store executes is this text exactly:
// the generated identitydb package carries it per dialect, with the consumer's
// table prefix substituted once at construction.
func Render(d dialect.Dialect) string {
	g := querygen.For(d)

	var rendered []*querygen.Query
	for _, table := range Emitted {
		rendered = append(rendered, g.StandardCRUD(table.Name, table.Columns, table.Options()...)...)
	}

	rendered = append(rendered, keyedInvitationLists(g)...)
	rendered = append(rendered, fieldWrites(g)...)

	return querygen.RenderFile(rendered)
}

// keyedInvitationLists is the two paged invitation reads the store actually
// runs: pending invitations from one user, and pending invitations addressed to
// one email. They are list variants rather than standard queries — a keyed
// column and a status predicate on top of the standard list — and before they
// were rendered here, the canonical .sql carried only the unkeyed list while
// the store executed these, which is exactly the checked-versus-executed gap
// the canonical file exists to close.
//
// The status is a bound argument rather than the literal 'pending', for the
// same reason the store binds it: a quoted literal in SQL text is one more
// place a status spelling lives.
func keyedInvitationLists(g *querygen.Generator) []*querygen.Query {
	scope := querygen.Match{Column: ScopeColumn}
	status := querygen.Match{Column: InvitationStatusColumn}

	return []*querygen.Query{
		g.ListQuery("ListInvitationsByFromUser", InvitationsTable, Invitations.Columns,
			scope, querygen.Match{Column: InvitationFromUserColumn}, status),
		g.ListQuery("ListInvitationsByToEmail", InvitationsTable, Invitations.Columns,
			scope, querygen.Match{Column: InvitationToEmailColumn}, status),
	}
}

// fieldWrites is the writes that assign one fact about a row rather than the
// row, plus the three that guard the assignment on the value being replaced.
//
// They are the largest block of statements this schema has and the standard set
// contains none of them, because "assign every mutable column" is the wrong
// shape for all of them. A password change writes the hash, the stamp and the
// forced-change release together, and must not touch a profile column; a status
// move writes the status and its explanation, and must not touch a credential.
// The struct a caller is holding is often a redacted copy, so a whole-row write
// reached from one of these paths blanks whatever it left out.
//
// Three of them are guarded, and the guard is the mechanism rather than a
// belt-and-braces check:
//
//	MarkUserEmailAddressVerified  names the token in the predicate, so two
//	                              clicks on one verification link write once —
//	                              the second finds it already cleared
//	TransferAccountOwnership      names the owner being moved away from, so two
//	                              concurrent transfers cannot both succeed and
//	                              leave the account owned by whichever committed
//	                              last
//	AnswerInvitation              names the pending status, so two answers
//	                              produce one membership rather than an
//	                              acceptance overwritten by a rejection
//
// Each reports zero rows when it loses, which every caller reads as the entity
// not being there — see SQLStore's guardCount.
//
// The values the guards compare against are bound rather than written into the
// SQL as literals, for the reason the keyed invitation lists bind the status: a
// quoted literal is one more place a spelling lives.
func fieldWrites(g *querygen.Generator) []*querygen.Query {
	scope := querygen.Match{Column: ScopeColumn}

	return []*querygen.Query{
		g.UpdateQuery("UpdateUserPassword", UsersTable, Users.Columns,
			[]string{hashedPasswordColumn, requiresPasswordChangeColumn, passwordLastChangedAtColumn},
			Users.Nullable, scope),

		g.UpdateQuery("SetUserRequiresPasswordChange", UsersTable, Users.Columns,
			[]string{requiresPasswordChangeColumn}, Users.Nullable, scope),

		// The secret and its verification move together. Two statements would
		// leave a window in which a freshly issued secret reads as already
		// proven, which is a window in which a second factor is bypassed by
		// re-enrolling.
		g.UpdateQuery("UpdateUserTwoFactorSecret", UsersTable, Users.Columns,
			[]string{twoFactorColumn, twoFactorVerifiedAtColumn}, Users.Nullable, scope),

		g.UpdateQuery("SetUserEmailAddressVerificationToken", UsersTable, Users.Columns,
			[]string{emailVerificationTokenColumn}, Users.Nullable, scope),

		g.UpdateQuery("MarkUserEmailAddressVerified", UsersTable, Users.Columns,
			[]string{EmailAddressVerifiedAtColumn, emailVerificationTokenColumn}, Users.Nullable,
			scope,
			querygen.Match{Column: emailVerificationTokenColumn, Arg: currentEmailVerificationTokenArg}),

		g.UpdateQuery("UpdateUserAccountStatus", UsersTable, Users.Columns,
			[]string{accountStatusColumn, accountStatusExplanationColumn}, Users.Nullable, scope),

		g.UpdateQuery("TransferAccountOwnership", AccountsTable, Accounts.Columns,
			[]string{ownerUserIDColumn}, Accounts.Nullable,
			scope,
			querygen.Match{Column: ownerUserIDColumn, Arg: currentOwnerUserIDArg}),

		g.UpdateQuery("AnswerInvitation", InvitationsTable, Invitations.Columns,
			[]string{InvitationStatusColumn, invitationNoteColumn, invitationToUserColumn},
			Invitations.Nullable,
			scope,
			querygen.Match{Column: InvitationStatusColumn, Arg: currentInvitationStatusArg}),
	}
}

// FileName is the file one dialect's rendered queries are committed to.
//
// The _generated suffix is in the path rather than only in the header comment,
// because a path is what a reviewer sees in a diff, what CI's glob selects, and
// what a reader scanning this directory reads first — and these are the files
// whose answer to "this line is wrong" is to edit something else.
func FileName(d dialect.Dialect) string {
	return string(d) + "_generated.sql"
}
