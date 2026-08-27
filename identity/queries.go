package identity

import (
	"fmt"
	"strings"
	"time"

	"github.com/primandproper/platform-go/v13/database/ddl"
	"github.com/primandproper/platform-go/v13/database/dialect"
	"github.com/primandproper/platform-go/v13/database/querygen"
	"github.com/primandproper/platform-go/v13/identity/internal/queries"
	"github.com/primandproper/platform-go/v13/tenancy"
)

// A note on timestamps, because one dialect does something surprising.
//
// Every time this package binds is a UTC time.Time, and every comparison is
// against another such value. Postgres and MySQL store these as real temporal
// types and compare them as such. SQLite does not: modernc's driver stores a
// bound time.Time as Go's own String() rendering — "2026-07-30 12:00:00 +0000
// UTC" — so `expires_at <= ?` there is a string comparison.
//
// That is still correct, because the rendering begins with a fixed-width
// "YYYY-MM-DD HH:MM:SS" prefix and everything is UTC, so lexical order is
// chronological order. It stops being correct the moment a value is bound in a
// non-UTC location, so do not remove the .UTC() calls at the binding sites.
//
// created_at is no longer one of them. The database writes it, from its own
// CURRENT_TIMESTAMP, which on SQLite is the shape that column's comparisons are
// lexicographic over rather than one that happens to sort right — see
// identity/migrations.

// tables holds the seven rendered table names. Derived from one prefix so a
// consumer cannot name them inconsistently — see identity/migrations.
type tables struct {
	base            string
	users           string
	userRoles       string
	accounts        string
	memberships     string
	membershipRoles string
	invitations     string
	invitationRoles string
}

func newTables(prefix string) *tables {
	q := ddl.Qualify(prefix)

	return &tables{
		base:            prefix,
		users:           q + queries.UsersTable,
		userRoles:       q + queries.UserRolesTable,
		accounts:        q + queries.AccountsTable,
		memberships:     q + queries.MembershipsTable,
		membershipRoles: q + queries.MembershipRolesTable,
		invitations:     q + queries.InvitationsTable,
		invitationRoles: q + queries.InvitationRolesTable,
	}
}

// prefix returns the prefix the names were derived from, for the validation
// that has to run against every rendered name rather than against any one.
func (t *tables) prefix() string { return t.base }

// The projections the hand-written reads below name, rendered from the column
// lists in identity/internal/queries.
//
// They used to be four comma-separated constants, and the scan side of each was
// a different file — which meant a column added to one and not the other was a
// runtime scan error rather than a compile error. Now there is one list per
// table and three consumers read it: these projections, the scan targets in
// scan.go, and the statements querygen renders. A column added to the list
// reaches all three, and a column added to only one of these strings is not
// something there is a string to add it to.
var (
	userProjection       = projection(queries.Users.Columns)
	accountProjection    = projection(queries.Accounts.Columns)
	membershipProjection = projection(queries.Memberships.Columns)
	invitationProjection = projection(queries.Invitations.Columns)
)

// projection renders a column list as a SELECT list.
func projection(columns []string) string {
	return strings.Join(columns, ", ")
}

// prefixColumns qualifies a column list with a table alias, for the joins whose
// projection names two tables at once.
func prefixColumns(alias string, columns []string) string {
	return projection(querygen.QualifyAll(alias, columns))
}

// nullableString maps an empty string to a SQL NULL, for the columns where "not
// set" and "set to nothing" are different facts — a subscription plan that was
// cancelled versus one named by the empty string.
func nullableString(s string) any {
	if s == "" {
		return nil
	}

	return s
}

// binder accumulates a statement's arguments and hands back the placeholder for
// each as it goes.
//
// It exists because hand-numbering placeholders is wrong in a way that only one
// of the three dialects notices. Postgres names its arguments — $1 written twice
// binds one value twice — so a statement that stamps `now` into two columns can
// reuse the index and works. SQLite and MySQL are positional: a second `?`
// consumes a second argument, so the same statement silently runs off the end of
// the argument list, and every argument after the reused one is bound to the
// wrong column. The failure is a driver error on two dialects and correct
// behavior on the third, which is the worst possible distribution.
//
// Binding through this type makes the mistake unspellable: every placeholder
// comes from an append, so the count and the numbering cannot disagree, and a
// value needed twice is simply bound twice.
//
// It is on its way out. Every statement database/querygen renders has its
// placeholders numbered by the renderer, over the finished text, which is the
// same property with nobody left to remember it — so the conventional writes
// against users, accounts and invitations, and now the field-specific and
// guarded ones beside them, no longer come through here. The password, the
// flags, the two-factor secret, the verification token, the account status, the
// ownership transfer and the invitation answer are all
// querygen.Generator.UpdateQuery statements in the canonical .sql, executed
// through the generated querier.
//
// What is left are the statements querygen still does not emit, and they are
// worth naming because each is a shape the epic behind this port owes a
// generator:
//
//	buildMarkTwoFactorVerified     an update whose guard is not an equality —
//	                               a secret that exists and has not been
//	                               proven, which is a `<> ''` and an IS NULL
//	buildRecordAgreements          an update whose SET list is chosen per call
//	buildUpdateAccountBilling      the same, over the billing columns
//	buildEraseUser                 a hard DELETE rather than an update
//	the membership writes          keyed on the (user, account) pair rather
//	                               than on id, an upsert that revives, and a
//	                               default-flag clear whose predicate excludes
//	                               a row rather than matching one
//	the prefix search              a LIKE with an ESCAPE and one conditional
//	                               cursor predicate
type binder struct {
	d    dialect.Dialect
	args []any
}

func newBinder(d dialect.Dialect) *binder {
	return &binder{d: d}
}

// bind appends a value and returns the placeholder that reads it.
func (b *binder) bind(value any) string {
	b.args = append(b.args, value)

	return b.d.Placeholder(len(b.args))
}

// ---------------------------------------------------------------- users

// likeEscape is the character the prefix search escapes wildcards with.
//
// Deliberately not a backslash. A backslash is itself an escape inside a string
// literal on MySQL and MariaDB unless NO_BACKSLASH_ESCAPES is set, so `ESCAPE
// '\'` is a syntax error there and `ESCAPE '\\'` is one on a server that has
// the mode set — there is no spelling of it that is right on both. An exclamation
// mark is ordinary in every dialect's string literal, and likePrefix escapes it
// in the pattern like any other special character.
const likeEscape = "!"

// buildSearchUsers renders the username prefix search.
//
// The pattern is built here rather than by the caller so that a prefix
// containing % or _ cannot become a wildcard: those are escaped, and the ESCAPE
// clause names the escape character explicitly rather than relying on a server
// default. Without this a search for "a%" matches the whole directory, which
// reads as a working search returning too much rather than as a bug.
func (t *tables) buildSearchUsers(d dialect.Dialect, scope tenancy.Scope, prefix, cursor string, limit int) (query string, args []any) {
	args = []any{scope, likePrefix(prefix)}

	where := fmt.Sprintf(
		"scope = %s AND archived_at IS NULL AND username LIKE %s ESCAPE '"+likeEscape+"'",
		d.Placeholder(1), d.Placeholder(2),
	)

	if cursor != "" {
		args = append(args, cursor)
		where += " AND username > " + d.Placeholder(len(args))
	}

	args = append(args, limit)

	return fmt.Sprintf(
		"SELECT %s FROM %s WHERE %s ORDER BY username LIMIT %s",
		userProjection, t.users, where, d.Placeholder(len(args)),
	), args
}

// buildCountSearchUsers counts what buildSearchUsers pages over.
func (t *tables) buildCountSearchUsers(d dialect.Dialect, scope tenancy.Scope, prefix string) (query string, args []any) {
	return fmt.Sprintf(
		"SELECT COUNT(*) FROM %s WHERE scope = %s AND archived_at IS NULL AND username LIKE %s ESCAPE '"+likeEscape+"'",
		t.users, d.Placeholder(1), d.Placeholder(2),
	), []any{scope, likePrefix(prefix)}
}

// likePrefix turns a literal prefix into a LIKE pattern, escaping the two
// wildcards and the escape character itself.
//
// strings.NewReplacer scans the input once and never re-examines what it has
// written, so the escape character's own rule cannot double the escapes the
// other two rules introduce — which a sequence of Replace calls would.
func likePrefix(prefix string) string {
	replaced := strings.NewReplacer(
		likeEscape, likeEscape+likeEscape,
		"%", likeEscape+"%",
		"_", likeEscape+"_",
	).Replace(prefix)

	return replaced + "%"
}

// buildSelectUserIDByField renders the collision check CreateUser and UpdateUser
// run before writing, so a taken username reports ErrUsernameTaken rather than a
// driver's constraint violation.
//
// exceptID excludes the row being updated, which is what lets a user save their
// profile without colliding with themselves.
func (t *tables) buildSelectUserIDByField(d dialect.Dialect, column string, scope tenancy.Scope, value, exceptID string) (query string, args []any) {
	args = []any{value, scope}

	where := fmt.Sprintf("%s = %s AND scope = %s", column, d.Placeholder(1), d.Placeholder(2))
	if exceptID != "" {
		args = append(args, exceptID)
		where += " AND id <> " + d.Placeholder(len(args))
	}

	return fmt.Sprintf("SELECT id FROM %s WHERE %s", t.users, where), args
}

// buildMarkTwoFactorVerified stamps a secret as proven, only where one exists
// and has not already been proven — so a replayed verification writes nothing
// and reports zero rows rather than moving the timestamp forward.
func (t *tables) buildMarkTwoFactorVerified(d dialect.Dialect, scope tenancy.Scope, userID string, now time.Time) (query string, args []any) {
	b := newBinder(d)

	return fmt.Sprintf(
		"UPDATE %s SET two_factor_secret_verified_at = %s, last_updated_at = %s "+
			"WHERE id = %s AND scope = %s AND archived_at IS NULL "+
			"AND two_factor_secret <> '' AND two_factor_secret_verified_at IS NULL",
		t.users, b.bind(now), b.bind(now), b.bind(userID), b.bind(scope),
	), b.args
}

// agreementColumns maps each agreement to the column that records it. It is a
// map rather than a switch at the call site so that adding a document means
// adding a column and an entry, and the set the Store validates against is the
// set the statement can write.
var agreementColumns = map[Agreement]string{
	TermsOfService: "last_accepted_terms_of_service",
	PrivacyPolicy:  "last_accepted_privacy_policy",
}

// buildRecordAgreements stamps one or more acceptances in a single statement,
// so accepting both documents at registration is one round trip.
func (t *tables) buildRecordAgreements(d dialect.Dialect, scope tenancy.Scope, userID string, agreements []Agreement, now time.Time) (query string, args []any) {
	b := newBinder(d)

	assignments := make([]string, 0, len(agreements)+1)
	for _, agreement := range agreements {
		assignments = append(assignments, agreementColumns[agreement]+" = "+b.bind(now))
	}

	assignments = append(assignments, "last_updated_at = "+b.bind(now))

	return fmt.Sprintf(
		"UPDATE %s SET %s WHERE id = %s AND scope = %s AND archived_at IS NULL",
		t.users, strings.Join(assignments, ", "), b.bind(userID), b.bind(scope),
	), b.args
}

// buildEraseUser destroys the row. Memberships and their roles go with it
// through ON DELETE CASCADE, which is the one place this schema relies on the
// database to finish a deletion — an erasure that left a membership behind
// would leave the subject's account list intact.
func (t *tables) buildEraseUser(d dialect.Dialect, scope tenancy.Scope, userID string) (query string, args []any) {
	return fmt.Sprintf("DELETE FROM %s WHERE id = %s AND scope = %s", t.users, d.Placeholder(1), d.Placeholder(2)),
		[]any{userID, scope}
}

// ---------------------------------------------------------------- accounts

// buildListAccountsForUser pages the accounts a user is a live member of.
//
// The membership's archived_at is checked as well as the account's: a user
// removed from an account they are still nominally listed against would
// otherwise keep seeing it in their switcher.
func (t *tables) buildListAccountsForUser(d dialect.Dialect, scope tenancy.Scope, userID, cursor string, limit int) (query string, args []any) {
	args = []any{scope, userID}

	where := fmt.Sprintf(
		"a.scope = %s AND m.belongs_to_user = %s AND a.archived_at IS NULL AND m.archived_at IS NULL",
		d.Placeholder(1), d.Placeholder(2),
	)

	if cursor != "" {
		args = append(args, cursor)
		where += " AND a.id > " + d.Placeholder(len(args))
	}

	args = append(args, limit)

	return fmt.Sprintf(
		"SELECT %s FROM %s AS a INNER JOIN %s AS m ON m.belongs_to_account = a.id "+
			"WHERE %s ORDER BY a.id LIMIT %s",
		prefixColumns("a", queries.Accounts.Columns), t.accounts, t.memberships,
		where, d.Placeholder(len(args)),
	), args
}

// buildCountAccountsForUser counts what buildListAccountsForUser pages over.
func (t *tables) buildCountAccountsForUser(d dialect.Dialect, scope tenancy.Scope, userID string) (query string, args []any) {
	return fmt.Sprintf(
		"SELECT COUNT(*) FROM %s AS a INNER JOIN %s AS m ON m.belongs_to_account = a.id "+
			"WHERE a.scope = %s AND m.belongs_to_user = %s AND a.archived_at IS NULL AND m.archived_at IS NULL",
		t.accounts, t.memberships, d.Placeholder(1), d.Placeholder(2),
	), []any{scope, userID}
}

// buildUpdateAccountBilling writes only the fields the update names, so a
// processor webhook carrying a status alone does not read-modify-write the rest
// and lose what another webhook did in between.
func (t *tables) buildUpdateAccountBilling(d dialect.Dialect, scope tenancy.Scope, accountID string, u *BillingUpdate, now time.Time) (query string, args []any) {
	assignments := make([]string, 0, 5)

	if u.Status != nil {
		args = append(args, u.Status.String())
		assignments = append(assignments, "billing_status = "+d.Placeholder(len(args)))
	}

	if u.SubscriptionPlanID != nil {
		args = append(args, nullableString(*u.SubscriptionPlanID))
		assignments = append(assignments, "subscription_plan_id = "+d.Placeholder(len(args)))
	}

	if u.PaymentProcessorCustomerID != nil {
		args = append(args, *u.PaymentProcessorCustomerID)
		assignments = append(assignments, "payment_processor_customer_id = "+d.Placeholder(len(args)))
	}

	if u.SyncedAt != nil {
		args = append(args, u.SyncedAt.UTC())
		assignments = append(assignments, "last_payment_provider_synced_at = "+d.Placeholder(len(args)))
	}

	args = append(args, now)
	assignments = append(assignments, "last_updated_at = "+d.Placeholder(len(args)))

	args = append(args, accountID)
	where := "id = " + d.Placeholder(len(args))

	args = append(args, scope)
	where += " AND scope = " + d.Placeholder(len(args))

	return fmt.Sprintf(
		"UPDATE %s SET %s WHERE %s AND archived_at IS NULL",
		t.accounts, strings.Join(assignments, ", "), where,
	), args
}

// ------------------------------------------------------------ memberships

// buildUpsertMembership writes a membership, reviving an archived one for the
// same pair rather than writing a second row.
//
// Reviving is what makes rejoining an account work: the pair is unique across
// live and archived rows, so an INSERT would fail and a DELETE-then-INSERT would
// lose when the user first joined. created_at is deliberately not updated —
// rejoining does not make the relationship new.
func (t *tables) buildUpsertMembership(d dialect.Dialect, m *Membership, now time.Time) (query string, args []any) {
	args = []any{m.ID, m.Scope, m.BelongsToUser, m.BelongsToAccount, m.DefaultAccount, now}

	base := fmt.Sprintf(
		"INSERT INTO %s (id, scope, belongs_to_user, belongs_to_account, default_account, created_at) VALUES (%s)",
		t.memberships, d.Placeholders(1, len(args)),
	)

	switch d {
	case dialect.MySQL:
		return base + " ON DUPLICATE KEY UPDATE" +
			" default_account = VALUES(default_account), archived_at = NULL," +
			" last_updated_at = " + d.Placeholder(len(args)+1), append(args, now)
	case dialect.Postgres, dialect.SQLite:
		return base + " ON CONFLICT (belongs_to_user, belongs_to_account) DO UPDATE SET" +
			" default_account = EXCLUDED.default_account, archived_at = NULL," +
			" last_updated_at = " + d.Placeholder(len(args)+1), append(args, now)
	default:
		return base, args
	}
}

// buildListMembershipsForUser reads every live membership a user holds, default
// account first — so a caller that takes the first row gets the one the user
// lands in.
func (t *tables) buildListMembershipsForUser(d dialect.Dialect, scope tenancy.Scope, userID string) (query string, args []any) {
	return fmt.Sprintf(
		"SELECT %s FROM %s WHERE scope = %s AND belongs_to_user = %s AND archived_at IS NULL "+
			"ORDER BY default_account DESC, belongs_to_account",
		membershipProjection, t.memberships, d.Placeholder(1), d.Placeholder(2),
	), []any{scope, userID}
}

// buildListAccountMembers pages an account's roster, joining each membership to
// the user it names so a page of thirty members is one query rather than
// thirty-one.
func (t *tables) buildListAccountMembers(d dialect.Dialect, scope tenancy.Scope, accountID, cursor string, limit int) (query string, args []any) {
	args = []any{scope, accountID}

	where := fmt.Sprintf(
		"m.scope = %s AND m.belongs_to_account = %s AND m.archived_at IS NULL AND u.archived_at IS NULL",
		d.Placeholder(1), d.Placeholder(2),
	)

	if cursor != "" {
		args = append(args, cursor)
		where += " AND m.id > " + d.Placeholder(len(args))
	}

	args = append(args, limit)

	return fmt.Sprintf(
		"SELECT %s, %s FROM %s AS m INNER JOIN %s AS u ON u.id = m.belongs_to_user "+
			"WHERE %s ORDER BY m.id LIMIT %s",
		prefixColumns("m", queries.Memberships.Columns), prefixColumns("u", queries.Users.Columns),
		t.memberships, t.users, where, d.Placeholder(len(args)),
	), args
}

// buildCountAccountMembers counts what buildListAccountMembers pages over.
func (t *tables) buildCountAccountMembers(d dialect.Dialect, scope tenancy.Scope, accountID string) (query string, args []any) {
	return fmt.Sprintf(
		"SELECT COUNT(*) FROM %s AS m INNER JOIN %s AS u ON u.id = m.belongs_to_user "+
			"WHERE m.scope = %s AND m.belongs_to_account = %s AND m.archived_at IS NULL AND u.archived_at IS NULL",
		t.memberships, t.users, d.Placeholder(1), d.Placeholder(2),
	), []any{scope, accountID}
}

// buildDeleteRoles clears an owner's roles against either role table, so a role
// set can be replaced wholesale rather than diffed. Diffing would mean reading
// the current set first and computing two statements from it, which is three
// round trips to express "these are the roles now".
func buildDeleteRoles(d dialect.Dialect, table, idColumn, ownerID string) (query string, args []any) {
	return fmt.Sprintf("DELETE FROM %s WHERE %s = %s", table, idColumn, d.Placeholder(1)),
		[]any{ownerID}
}

// buildInsertRoles renders one multi-row INSERT for a set of roles against
// either role table, so granting six roles costs one round trip rather than six.
func buildInsertRoles(d dialect.Dialect, table, idColumn, ownerID string, roles []string) (query string, args []any) {
	const columnsPerRow = 2

	args = make([]any, 0, len(roles)*columnsPerRow)
	tuples := make([]string, 0, len(roles))

	for _, role := range roles {
		tuples = append(tuples, "("+d.Placeholders(len(args)+1, columnsPerRow)+")")
		args = append(args, ownerID, role)
	}

	return fmt.Sprintf(
		"INSERT INTO %s (%s, role) VALUES %s",
		table, idColumn, strings.Join(tuples, ", "),
	), args
}

// buildSelectRoles reads the roles for a batch of owners at once.
//
// Batched rather than one query per membership, which is the shape webhooks
// reads subscriptions in and the shape a roster page cannot afford: thirty
// members would be thirty round trips, each returning two rows.
func buildSelectRoles(d dialect.Dialect, table, idColumn string, ownerIDs []string) (query string, args []any) {
	args = make([]any, 0, len(ownerIDs))
	for _, id := range ownerIDs {
		args = append(args, id)
	}

	return fmt.Sprintf(
		"SELECT %s, role FROM %s WHERE %s IN (%s) ORDER BY %s, role",
		idColumn, table, idColumn, d.Placeholders(1, len(args)), idColumn,
	), args
}

// buildClearDefaultAccount clears the default flag from a user's other
// memberships, so "one default per user" is an invariant of the write rather
// than of the caller remembering to.
func (t *tables) buildClearDefaultAccount(d dialect.Dialect, scope tenancy.Scope, userID, exceptAccountID string, now time.Time) (query string, args []any) {
	b := newBinder(d)

	return fmt.Sprintf(
		"UPDATE %s SET default_account = FALSE, last_updated_at = %s "+
			"WHERE scope = %s AND belongs_to_user = %s AND belongs_to_account <> %s AND default_account = TRUE",
		t.memberships, b.bind(now), b.bind(scope), b.bind(userID), b.bind(exceptAccountID),
	), b.args
}

// buildSetDefaultAccount marks one live membership as the user's default.
func (t *tables) buildSetDefaultAccount(d dialect.Dialect, scope tenancy.Scope, userID, accountID string, now time.Time) (query string, args []any) {
	b := newBinder(d)

	return fmt.Sprintf(
		"UPDATE %s SET default_account = TRUE, last_updated_at = %s "+
			"WHERE scope = %s AND belongs_to_user = %s AND belongs_to_account = %s AND archived_at IS NULL",
		t.memberships, b.bind(now), b.bind(scope), b.bind(userID), b.bind(accountID),
	), b.args
}

// buildArchiveMembership ends one user's membership in one account.
func (t *tables) buildArchiveMembership(d dialect.Dialect, scope tenancy.Scope, userID, accountID string, now time.Time) (query string, args []any) {
	b := newBinder(d)

	return fmt.Sprintf(
		"UPDATE %s SET archived_at = %s, default_account = FALSE, last_updated_at = %s "+
			"WHERE scope = %s AND belongs_to_user = %s AND belongs_to_account = %s AND archived_at IS NULL",
		t.memberships, b.bind(now), b.bind(now),
		b.bind(scope), b.bind(userID), b.bind(accountID),
	), b.args
}

// buildArchiveMembershipsBy ends every live membership matching one side of the
// relationship, for archiving a user or an account. The column is this
// package's own constant, never a caller's string.
func (t *tables) buildArchiveMembershipsBy(d dialect.Dialect, column string, scope tenancy.Scope, id string, now time.Time) (query string, args []any) {
	b := newBinder(d)

	return fmt.Sprintf(
		"UPDATE %s SET archived_at = %s, default_account = FALSE, last_updated_at = %s "+
			"WHERE scope = %s AND %s = %s AND archived_at IS NULL",
		t.memberships, b.bind(now), b.bind(now), b.bind(scope), column, b.bind(id),
	), b.args
}

// ----------------------------------------------------------- invitations

// buildCountLiveMembershipsForUser counts a user's live memberships, which is
// how CreateMembership finds out whether the one it is writing is their first
// and therefore their default.
func (t *tables) buildCountLiveMembershipsForUser(d dialect.Dialect, scope tenancy.Scope, userID string) (query string, args []any) {
	return fmt.Sprintf(
		"SELECT COUNT(*) FROM %s WHERE scope = %s AND belongs_to_user = %s AND archived_at IS NULL",
		t.memberships, d.Placeholder(1), d.Placeholder(2),
	), []any{scope, userID}
}

// buildSelectUsersByIDs reads a batch of users in one query.
//
// It has no archived clause: a caller hydrating references is naming users that
// other rows already point at, and hiding a soft-deleted one turns "created by
// a departed colleague" into "created by nobody".
func (t *tables) buildSelectUsersByIDs(d dialect.Dialect, scope tenancy.Scope, userIDs []string) (query string, args []any) {
	args = make([]any, 0, len(userIDs)+1)
	for _, id := range userIDs {
		args = append(args, id)
	}

	placeholders := d.Placeholders(1, len(userIDs))
	args = append(args, scope)

	return fmt.Sprintf(
		"SELECT %s FROM %s WHERE id IN (%s) AND scope = %s ORDER BY id",
		userProjection, t.users, placeholders, d.Placeholder(len(args)),
	), args
}
