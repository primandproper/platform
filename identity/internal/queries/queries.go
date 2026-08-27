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

// The user columns the keyed sign-in reads name. Exported for the same reason
// the invitation columns above are: the store spells them too, and two
// spellings of one column is the drift this package exists to prevent.
const (
	UserUsernameColumn               = "username"
	UserEmailAddressColumn           = "email_address"
	UserEmailVerificationTokenColumn = "email_address_verification_token"
)

// The two columns a membership is keyed on, unique on the pair live and
// archived alike. Together they are the row's natural key — one live membership
// per (user, account) pair — which is why every membership statement addresses
// a row by both rather than by the id the table also carries, and why the
// upsert converges on them: the schema's UNIQUE, the upsert's ON CONFLICT and
// the store's own reads all name these two columns, and a conflict target that
// disagreed with the key would insert a second row where it meant to revive the
// first.
const (
	MembershipUserColumn    = "belongs_to_user"
	MembershipAccountColumn = querygen.BelongsToAccountColumn
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
	ownerUserIDColumn              = "owner_user_id"
	invitationNoteColumn           = "note"
	invitationToUserColumn         = "to_user"

	currentEmailVerificationTokenArg = "current_" + UserEmailVerificationTokenColumn
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
		UserUsernameColumn,
		UserEmailAddressColumn,
		"first_name",
		"last_name",
		"hashed_password",
		"requires_password_change",
		"password_last_changed_at",
		"two_factor_secret",
		"two_factor_secret_verified_at",
		EmailAddressVerifiedAtColumn,
		UserEmailVerificationTokenColumn,
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
		UserUsernameColumn,
		UserEmailAddressColumn,
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
// It gets no standard queries — every one of its statements keys on the (user,
// account) pair rather than on the id — but the three keyed reads below and the
// upsert that revives an archived membership when a user rejoins are emitted
// from it, so its columns are the canonical corpus's as well as the store's.
//
// Updatable is the one column a rejoin may carry over, and stating it is what
// keeps the upsert's conflict branch off the rest: the id is what the
// membership's roles hang from and must survive a revival, and the scope is
// immutable here as it is everywhere else in this schema.
var Memberships = Table{
	Name:     MembershipsTable,
	Singular: "Membership",
	Plural:   "Memberships",
	Columns: []string{
		querygen.IDColumn,
		ScopeColumn,
		MembershipUserColumn,
		MembershipAccountColumn,
		"default_account",
		querygen.CreatedAtColumn,
		querygen.LastUpdatedAtColumn,
		querygen.ArchivedAtColumn,
	},
	Updatable: []string{"default_account"},
}

// Emitted is the tables the canonical .sql covers with the standard set, in the
// order they appear in it.
//
// Memberships is deliberately absent and still contributes one statement — see
// [membershipUpsert]. The list is what gets a set, not what gets a statement.
var Emitted = []*Table{&Users, &Accounts, &Invitations}

// Render returns the canonical sqlc input for d: every emitted table's standard
// queries, the two keyed invitation lists, and the membership upsert, in one
// file's worth of text.
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
	rendered = append(rendered, createdAtReads(g)...)
	rendered = append(rendered, keyedUserReads(g)...)
	rendered = append(rendered, keyedMembershipReads(g)...)
	rendered = append(rendered, fieldWrites(g)...)
	rendered = append(rendered, membershipUpsert(g))

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

// membershipUpsert is the write that puts a user in an account, and the one
// statement in this schema whose three renderings differ beyond their
// placeholders: Postgres and SQLite take ON CONFLICT on the pair, MySQL takes
// ON DUPLICATE KEY UPDATE and no target at all.
//
// It has to converge rather than insert, because the pair is unique across live
// and archived rows alike. A plain INSERT fails when a user rejoins an account
// they once left; a DELETE followed by an INSERT loses when they first joined
// and takes the membership's roles with it, since those hang off the id. So the
// conflict branch clears archived_at and leaves the id and created_at as they
// were, which is what makes a rejoin a revival of the old relationship.
//
// The conflict target is the key rather than the key plus the scope, and it is
// not free to be otherwise: Postgres matches ON CONFLICT against a unique index
// the table actually has, and this schema's is on the pair. Nothing is lost by
// it — a user and an account are each scoped rows, so a pair naming both has
// already named one directory — and the scope is not assigned in the conflict
// branch, so a converging write cannot move a membership between them.
func membershipUpsert(g *querygen.Generator) *querygen.Query {
	return g.UpsertQuery("UpsertMembership", MembershipsTable,
		Memberships.Columns,
		Memberships.InsertColumns(),
		Memberships.UpdateColumns(),
		Memberships.Nullable,
		querygen.Match{Column: MembershipUserColumn},
		querygen.Match{Column: MembershipAccountColumn},
	)
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
			[]string{UserEmailVerificationTokenColumn}, Users.Nullable, scope),

		g.UpdateQuery("MarkUserEmailAddressVerified", UsersTable, Users.Columns,
			[]string{EmailAddressVerifiedAtColumn, UserEmailVerificationTokenColumn}, Users.Nullable,
			scope,
			querygen.Match{Column: UserEmailVerificationTokenColumn, Arg: currentEmailVerificationTokenArg}),

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

// createdAtReads is the read-back of the one column an emitted table's create
// does not carry: the creation time the database assigned it.
//
// created_at is database-owned — it is not in any create's column list, and the
// schema gives it a DEFAULT — so the value the caller handed over still holds
// the zero time when the INSERT returns, and the store reads it back inside the
// same transaction. One per emitted table, because a query name is a Go method
// name and the table is not a parameter of one.
//
// It keys on the id alone. The scope is absent because this is not a read a
// caller reaches: it is the create's read-back of the row it has just written,
// by the id it minted for it, and the row is not visible to anything else until
// the transaction commits. The column list is the id and nothing else, which is
// also what leaves the archived predicate off a row that cannot be archived yet.
func createdAtReads(g *querygen.Generator) []*querygen.Query {
	rendered := make([]*querygen.Query, 0, len(Emitted))

	for _, table := range Emitted {
		rendered = append(rendered, g.ReadQuery(
			"Get"+table.Singular+"CreatedAt", table.Name,
			[]string{querygen.IDColumn},
			querygen.Read{Projection: []string{querygen.CreatedAtColumn}},
		))
	}

	return rendered
}

// keyedUserReads is the three single-user reads that key on something other than
// the id: the two sign-in reads and the one behind an email verification.
//
// They were one builder parameterized on the column, which is not a thing a
// query name can be — sqlc turns a name into a Go method — so they enumerate
// here into one named read each. What the builder was protecting is preserved
// by construction instead: the scope predicate and the archived clause are
// querygen's, rendered from one column list, so the sign-in read cannot be the
// one that forgot either.
func keyedUserReads(g *querygen.Generator) []*querygen.Query {
	read := querygen.Read{Projection: Users.Columns}

	// Statement name to the column it keys on, in the order the file lists
	// them. A map would lose that order, and the .sql is compared byte for byte
	// against its committed copy.
	named := [][2]string{
		{"GetUserByUsername", UserUsernameColumn},
		{"GetUserByEmailAddress", UserEmailAddressColumn},
		{"GetUserByEmailVerificationToken", UserEmailVerificationTokenColumn},
	}

	rendered := make([]*querygen.Query, 0, len(named))
	for i := range named {
		rendered = append(rendered, g.ReadQuery(named[i][0], UsersTable, Users.KeyedColumns(), read,
			querygen.Match{Column: named[i][1]}, querygen.Match{Column: ScopeColumn}))
	}

	return rendered
}

// keyedMembershipReads is the three reads over the table that has no standard
// queries at all: the membership itself, the id a revived one kept, and the
// account a user falls back to when the default is being removed.
//
// All three key on the (user, account) pair rather than on the id the table
// carries, which is why each is rendered from Memberships.KeyedColumns() while
// projecting from Memberships.Columns.
func keyedMembershipReads(g *querygen.Generator) []*querygen.Query {
	var (
		columns = Memberships.KeyedColumns()
		scope   = querygen.Match{Column: ScopeColumn}
		user    = querygen.Match{Column: MembershipUserColumn}
		account = querygen.Match{Column: MembershipAccountColumn}
	)

	return []*querygen.Query{
		g.ReadQuery("GetMembershipByUserAndAccount", MembershipsTable, columns,
			querygen.Read{Projection: Memberships.Columns}, scope, user, account),

		// The id the row actually carries, which is not always the id the
		// caller generated: a user rejoining an account revives the archived
		// membership, and it keeps the id it was created with. The roles are
		// written against this one.
		g.ReadQuery("GetMembershipIDByUserAndAccount", MembershipsTable, columns,
			querygen.Read{Projection: []string{querygen.IDColumn}}, scope, user, account),

		// Another live membership for the user, for moving the default off the
		// one being removed — so a user is never left with memberships and
		// nowhere to land. The account is excluded rather than matched, and the
		// order is what makes "another" a row rather than whichever one the
		// planner reached first.
		g.ReadQuery("GetMembershipFallbackAccountID", MembershipsTable, columns,
			querygen.Read{
				Projection: []string{MembershipAccountColumn},
				Order:      MembershipAccountColumn,
			},
			scope, user, querygen.Match{Column: MembershipAccountColumn, Exclude: true}),
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
