/*
Package queries is the identity schema described as data: the canonical table
names, each table's columns in the order every read projects them, and the two
subsets a write may assign.

It exists because those facts now have two consumers that must not disagree.
The identity store renders them through database/querygen's Bound methods, with
the consumer's table prefix on the name, and executes what comes back; the
generator behind `make generate` renders the same tables through
[querygen.Generator.StandardCRUD] into the canonical .sql files sqlc is run
over. A column list spelled in both places could differ in one name, and the
symptom would be a check that passes over SQL nobody executes.

So it is spelled once, here, and both halves read it. The .sql files beside this
file are the generator's output — see [Render] and identity/internal/queriesgen.

# Which queries each table gets

[Table.Options] is where a table says what it is beyond a list of columns, and
three of the four options carry a fact that a column list cannot:

  - Ownership is the scope column, so every emitted statement is keyed on it.
    It is named rather than inferred, because a table whose rows are readable
    across scopes and one whose rows are not look identical from the columns.
  - Nullable names the columns a write may set to NULL, which lives in the
    schema neither this package nor querygen reads.
  - Updatable names the columns the standard update assigns, and everything
    else assignable becomes immutable to it. It is stated positively because
    that is the shorter and the more checkable half: a user has four profile
    columns and ten written only by the method that owns them, and a list of
    the ten is a list somebody adds a column to a table without extending.
    Getting it wrong is not a small thing — querygen assigns every column its
    options leave mutable, and the struct a caller is holding is often a
    [identity.User.Redacted] copy whose credential fields are empty, so a
    password hash left in the update set is blanked on every profile save.

Memberships is the fourth table and is deliberately not emitted. Its columns are
textbook and not one of its statements is: the get, the archive and the bulk
archive key on the (belongs_to_user, belongs_to_account) pair rather than on id,
and its write is an upsert that revives an archived row. It is declared here
anyway, because the store still projects its columns and one list is the point.
*/
package queries

import (
	"slices"

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

// EmailAddressVerifiedAtColumn is the proof a user's address is reachable.
//
// Spelled once because three declarations below name it — it is nullable, it is
// updatable, and it is in the projection — and because the store reasons about
// its value rather than only assigning it: moving an address clears it.
const EmailAddressVerifiedAtColumn = "email_address_verified_at"

// Table is one identity table's shape.
//
// Columns is the full list, in the order the emitted SELECTs project it, which
// is also the order identity's scan targets are written in. The rest is what a
// column list cannot say — see the package comment.
type Table struct {
	// Name is the canonical, unprefixed table name.
	Name string
	// Singular and Plural name the entity the emitted query names are built
	// from: GetUser rather than GetIdentityUsers.
	Singular string
	Plural   string

	// Columns is every column, in projection order.
	Columns []string
	// Nullable names the columns a write may set to NULL.
	Nullable []string
	// Updatable names the columns the standard update assigns. Everything else
	// this table would otherwise let an update assign becomes immutable to it —
	// see Immutable.
	Updatable []string
	// Omitted names the standard queries this table has no caller for.
	Omitted []querygen.StandardQuery
}

// InsertColumns returns the columns the create supplies values for: everything
// but the database-owned ones.
//
// created_at is among those the database owns, which is the whole reason the
// schema gives it a DEFAULT — see identity/migrations. A caller-supplied
// creation time is how a row ends up with one that disagrees with its id, and
// the cursor walk orders by id while the filter window compares created_at.
func (t *Table) InsertColumns() []string {
	return querygen.ForInsert(t.Columns)
}

// Immutable returns the columns the standard update must not assign: everything
// assignable that Updatable does not name, plus the scope.
//
// Derived rather than declared, so that a column added to a table is immutable
// until somebody says otherwise. The other direction — a new column silently
// joining the update set — is the one with a failure mode, and it is the failure
// mode Updatable's doc describes.
func (t *Table) Immutable() []string {
	assignable := querygen.ForUpdate(t.Columns, ScopeColumn)

	immutable := make([]string, 0, len(assignable))
	for _, column := range assignable {
		if !slices.Contains(t.Updatable, column) {
			immutable = append(immutable, column)
		}
	}

	return immutable
}

// UpdateColumns returns the columns the standard update assigns, in projection
// order.
//
// Order matters because both consumers render from it: the store's Bound update
// and the canonical .sql have to assign the same columns in the same places, and
// deriving both from the column list is what makes that true rather than
// remembered.
func (t *Table) UpdateColumns() []string {
	return querygen.ForUpdate(t.Columns, append(t.Immutable(), ScopeColumn)...)
}

// Options renders this table's shape as the options StandardCRUD reads.
func (t *Table) Options() []querygen.Option {
	opts := []querygen.Option{
		querygen.WithEntity(t.Singular, t.Plural),
		querygen.WithOwnership(ScopeColumn),
		querygen.WithNullable(t.Nullable...),
		querygen.WithImmutable(t.Immutable()...),
	}

	if len(t.Omitted) > 0 {
		opts = append(opts, querygen.WithOmitted(t.Omitted...))
	}

	return opts
}

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
// and what CI regenerates to check the committed copy still matches. Nothing
// executes what it returns — the store renders the same statements through the
// Bound methods, with the prefix on the table name and the argument references
// rewritten into bind markers.
func Render(d dialect.Dialect) string {
	g := querygen.For(d)

	var rendered []*querygen.Query
	for _, table := range Emitted {
		rendered = append(rendered, g.StandardCRUD(table.Name, table.Columns, table.Options()...)...)
	}

	return querygen.RenderFile(rendered)
}
