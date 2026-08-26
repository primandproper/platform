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

// The pair a membership is unique on, live and archived alike — the key its
// statements address a row by, and the conflict target its upsert converges on.
//
// Spelled once for the reason the whole package is: the schema's UNIQUE, the
// upsert's ON CONFLICT and the store's own reads all name these two columns, and
// a conflict target that disagrees with the key is a write that inserts a second
// row where it meant to revive the first.
const (
	MembershipUserColumn    = "belongs_to_user"
	MembershipAccountColumn = "belongs_to_account"
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
// It gets no update and no archive. An invitation is answered rather than
// edited, and the answer is a status-guarded write — still pending in the
// predicate, so two clicks on one link produce one membership — which is not a
// shape this generator emits.
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
// It is declared for its columns and for one statement: the upsert that revives
// an archived membership when a user rejoins an account. Every other statement
// it needs keys on the pair rather than on the id, which is not a shape the
// standard set produces — see the package comment.
//
// Updatable is the one column a rejoin may carry over, and stating it is what
// keeps the conflict branch off the rest: the id is what the membership's roles
// hang from and must survive a revival, and the scope is immutable here as it is
// everywhere else in this schema.
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

// FileName is the file one dialect's rendered queries are committed to.
//
// The _generated suffix is in the path rather than only in the header comment,
// because a path is what a reviewer sees in a diff, what CI's glob selects, and
// what a reader scanning this directory reads first — and these are the files
// whose answer to "this line is wrong" is to edit something else.
func FileName(d dialect.Dialect) string {
	return string(d) + "_generated.sql"
}
