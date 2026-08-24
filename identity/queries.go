package identity

import (
	"fmt"
	"strings"
	"time"

	"github.com/primandproper/platform-go/v13/database/ddl"
	"github.com/primandproper/platform-go/v13/database/dialect"
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
		users:           q + "identity_users",
		userRoles:       q + "identity_user_roles",
		accounts:        q + "identity_accounts",
		memberships:     q + "identity_memberships",
		membershipRoles: q + "identity_membership_roles",
		invitations:     q + "identity_invitations",
		invitationRoles: q + "identity_invitation_roles",
	}
}

// prefix returns the prefix the names were derived from, for the validation
// that has to run against every rendered name rather than against any one.
func (t *tables) prefix() string { return t.base }

// The projections every read scans. Declared once each so the SELECTs and the
// Scans cannot drift apart — a column added to one and not the other is a scan
// error at runtime rather than a compile error, and the two lists are far apart
// in the file.
const (
	userColumns = "id, scope, username, email_address, first_name, last_name, " +
		"hashed_password, requires_password_change, password_last_changed_at, " +
		"two_factor_secret, two_factor_secret_verified_at, " +
		"email_address_verified_at, email_address_verification_token, " +
		"account_status, account_status_explanation, " +
		"last_accepted_terms_of_service, last_accepted_privacy_policy, " +
		"created_at, last_updated_at, archived_at"

	accountColumns = "id, scope, name, owner_user_id, billing_status, " +
		"subscription_plan_id, payment_processor_customer_id, last_payment_provider_synced_at, " +
		"address_line1, address_line2, address_city, address_state, " +
		"address_postal_code, address_country, address_phone, time_zone, " +
		"created_at, last_updated_at, archived_at"

	membershipColumns = "id, scope, belongs_to_user, belongs_to_account, default_account, " +
		"created_at, last_updated_at, archived_at"

	invitationColumns = "id, scope, belongs_to_account, from_user, to_email, to_name, to_user, " +
		"token, status, note, expires_at, created_at, last_updated_at, archived_at"
)

// prefixColumns qualifies a bare column list with a table alias, so a
// projection declared once can be reused in a join without repeating it.
func prefixColumns(prefix, columns string) string {
	parts := strings.Split(columns, ", ")
	for i := range parts {
		parts[i] = prefix + parts[i]
	}

	return strings.Join(parts, ", ")
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

// buildInsertUser renders the user write.
//
// It is a plain INSERT rather than an upsert: CreateUser creates, and the
// updates each have a method that writes only the columns they own. An upsert
// here would make "create" silently overwrite a user whose ID collided, which
// with generated IDs means never in testing and catastrophically in production.
func (t *tables) buildInsertUser(d dialect.Dialect, u *User, now time.Time) (query string, args []any) {
	args = []any{
		u.ID, u.Scope, u.Username, u.EmailAddress, u.FirstName, u.LastName,
		u.HashedPassword, u.RequiresPasswordChange, u.PasswordLastChangedAt,
		u.TwoFactorSecret, u.TwoFactorSecretVerifiedAt,
		u.EmailAddressVerifiedAt, u.EmailAddressVerificationToken,
		u.AccountStatus.String(), u.AccountStatusExplanation,
		u.LastAcceptedTermsOfService, u.LastAcceptedPrivacyPolicy,
		now,
	}

	return fmt.Sprintf(
		"INSERT INTO %s (id, scope, username, email_address, first_name, last_name, "+
			"hashed_password, requires_password_change, password_last_changed_at, "+
			"two_factor_secret, two_factor_secret_verified_at, "+
			"email_address_verified_at, email_address_verification_token, "+
			"account_status, account_status_explanation, "+
			"last_accepted_terms_of_service, last_accepted_privacy_policy, created_at) VALUES (%s)",
		t.users, d.Placeholders(1, len(args)),
	), args
}

// buildSelectUser renders the single-user read, within one scope. Archived
// users are returned — a soft-deleted user is still referenced by an audit row
// and by another domain's foreign key, and hiding them makes those dangle.
func (t *tables) buildSelectUser(d dialect.Dialect, scope tenancy.Scope, userID string) (query string, args []any) {
	return fmt.Sprintf(
		"SELECT %s FROM %s WHERE id = %s AND scope = %s",
		userColumns, t.users, d.Placeholder(1), d.Placeholder(2),
	), []any{userID, scope}
}

// buildSelectLiveUserBy renders the sign-in reads: by username, by email
// address, or by verification token, always live-only.
//
// The three share a builder because they differ in exactly one identifier and
// nothing else — the scope predicate, the archived clause, and the projection
// are the parts that must not differ, and three copies of them is three chances
// for the sign-in read to be the one that forgot the archived clause.
func (t *tables) buildSelectLiveUserBy(d dialect.Dialect, column string, scope tenancy.Scope, value string) (query string, args []any) {
	return fmt.Sprintf(
		"SELECT %s FROM %s WHERE %s = %s AND scope = %s AND archived_at IS NULL",
		userColumns, t.users, column, d.Placeholder(1), d.Placeholder(2),
	), []any{value, scope}
}

// buildListUsers renders the directory page for one scope, cursor-paginated on
// username — which is what the page is ordered by, so the cursor and the order
// agree. Paging on id while ordering on username skips and repeats rows.
func (t *tables) buildListUsers(d dialect.Dialect, scope tenancy.Scope, cursor string, limit int) (query string, args []any) {
	args = []any{scope}

	where := "scope = " + d.Placeholder(1) + " AND archived_at IS NULL"
	if cursor != "" {
		args = append(args, cursor)
		where += " AND username > " + d.Placeholder(len(args))
	}

	args = append(args, limit)

	return fmt.Sprintf(
		"SELECT %s FROM %s WHERE %s ORDER BY username LIMIT %s",
		userColumns, t.users, where, d.Placeholder(len(args)),
	), args
}

// buildCountUsers counts the scope's live users, for the page's total.
func (t *tables) buildCountUsers(d dialect.Dialect, scope tenancy.Scope) (query string, args []any) {
	return fmt.Sprintf(
		"SELECT COUNT(*) FROM %s WHERE scope = %s AND archived_at IS NULL",
		t.users, d.Placeholder(1),
	), []any{scope}
}

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
		userColumns, t.users, where, d.Placeholder(len(args)),
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

// buildUpdateUser writes the profile columns and nothing else. Changing the
// email address clears its verification, in the same statement — two statements
// would leave a window in which the new address reads as proven.
func (t *tables) buildUpdateUser(d dialect.Dialect, u *User, now time.Time) (query string, args []any) {
	b := newBinder(d)

	// The verification clause is assigned FIRST, and the order is load-bearing.
	//
	// MySQL evaluates a single-table UPDATE's assignments left to right and lets
	// later ones see the values earlier ones wrote; Postgres and SQLite evaluate
	// every assignment against the row as it was. So with email_address assigned
	// first, MySQL's CASE compares the new address against itself, finds them
	// equal, and keeps the verification — meaning a user could move their
	// address to one they have never proven and stay verified, on one dialect
	// only. Assigning the CASE before email_address makes all three read the old
	// value.
	return fmt.Sprintf(
		"UPDATE %s SET "+
			"email_address_verified_at = CASE WHEN email_address = %s THEN email_address_verified_at ELSE NULL END, "+
			"username = %s, email_address = %s, first_name = %s, last_name = %s, "+
			"last_updated_at = %s WHERE id = %s AND scope = %s AND archived_at IS NULL",
		t.users,
		b.bind(u.EmailAddress),
		b.bind(u.Username), b.bind(u.EmailAddress), b.bind(u.FirstName), b.bind(u.LastName),
		b.bind(now), b.bind(u.ID), b.bind(u.Scope),
	), b.args
}

// buildUpdateUserPassword writes the hash, stamps the change, and clears the
// forced-change flag in one statement — see Store.UpdateUserPassword for why
// clearing it here rather than leaving it to the caller is what makes a forced
// password change terminate.
func (t *tables) buildUpdateUserPassword(d dialect.Dialect, scope tenancy.Scope, userID, hashedPassword string, now time.Time) (query string, args []any) {
	b := newBinder(d)

	return fmt.Sprintf(
		"UPDATE %s SET hashed_password = %s, requires_password_change = FALSE, "+
			"password_last_changed_at = %s, last_updated_at = %s "+
			"WHERE id = %s AND scope = %s AND archived_at IS NULL",
		t.users, b.bind(hashedPassword), b.bind(now), b.bind(now),
		b.bind(userID), b.bind(scope),
	), b.args
}

// buildSetUserFlag renders the single-boolean-column writes.
func (t *tables) buildSetUserFlag(d dialect.Dialect, column string, scope tenancy.Scope, userID string, value bool, now time.Time) (query string, args []any) {
	b := newBinder(d)

	return fmt.Sprintf(
		"UPDATE %s SET %s = %s, last_updated_at = %s WHERE id = %s AND scope = %s AND archived_at IS NULL",
		t.users, column, b.bind(value), b.bind(now), b.bind(userID), b.bind(scope),
	), b.args
}

// buildUpdateTwoFactorSecret stores a new secret and clears its verification in
// the same statement. The two cannot be separate writes: a window in which a
// freshly issued secret reads as already proven is a window in which a second
// factor is bypassed by re-enrolling.
func (t *tables) buildUpdateTwoFactorSecret(d dialect.Dialect, scope tenancy.Scope, userID, secret string, now time.Time) (query string, args []any) {
	b := newBinder(d)

	return fmt.Sprintf(
		"UPDATE %s SET two_factor_secret = %s, two_factor_secret_verified_at = NULL, "+
			"last_updated_at = %s WHERE id = %s AND scope = %s AND archived_at IS NULL",
		t.users, b.bind(secret), b.bind(now), b.bind(userID), b.bind(scope),
	), b.args
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

// buildSetEmailVerificationToken replaces any outstanding token, so re-sending
// a verification email invalidates the previous link rather than leaving two
// live.
func (t *tables) buildSetEmailVerificationToken(d dialect.Dialect, scope tenancy.Scope, userID, token string, now time.Time) (query string, args []any) {
	b := newBinder(d)

	return fmt.Sprintf(
		"UPDATE %s SET email_address_verification_token = %s, last_updated_at = %s "+
			"WHERE id = %s AND scope = %s AND archived_at IS NULL",
		t.users, b.bind(token), b.bind(now), b.bind(userID), b.bind(scope),
	), b.args
}

// buildMarkEmailVerified stamps the address and clears the token, with the
// token in the predicate.
//
// Comparing it here rather than trusting the caller's earlier read is what makes
// two concurrent clicks on the same link write once: the second finds the token
// already cleared and matches nothing.
func (t *tables) buildMarkEmailVerified(d dialect.Dialect, scope tenancy.Scope, userID, token string, now time.Time) (query string, args []any) {
	b := newBinder(d)

	return fmt.Sprintf(
		"UPDATE %s SET email_address_verified_at = %s, email_address_verification_token = '', "+
			"last_updated_at = %s WHERE id = %s AND scope = %s AND archived_at IS NULL "+
			"AND email_address_verification_token = %s",
		t.users, b.bind(now), b.bind(now),
		b.bind(userID), b.bind(scope), b.bind(token),
	), b.args
}

// buildUpdateAccountStatus moves a user between statuses.
func (t *tables) buildUpdateAccountStatus(d dialect.Dialect, scope tenancy.Scope, userID string, status AccountStatus, explanation string, now time.Time) (query string, args []any) {
	b := newBinder(d)

	return fmt.Sprintf(
		"UPDATE %s SET account_status = %s, account_status_explanation = %s, last_updated_at = %s "+
			"WHERE id = %s AND scope = %s AND archived_at IS NULL",
		t.users, b.bind(status.String()), b.bind(explanation), b.bind(now),
		b.bind(userID), b.bind(scope),
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

// buildArchiveUser soft-deletes a user. Already-archived users are excluded, so
// a repeated archive does not move the timestamp and lose when it first
// happened.
func (t *tables) buildArchiveUser(d dialect.Dialect, scope tenancy.Scope, userID string, now time.Time) (query string, args []any) {
	b := newBinder(d)

	return fmt.Sprintf(
		"UPDATE %s SET archived_at = %s, last_updated_at = %s WHERE id = %s AND scope = %s AND archived_at IS NULL",
		t.users, b.bind(now), b.bind(now), b.bind(userID), b.bind(scope),
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

// buildInsertAccount renders the account write.
func (t *tables) buildInsertAccount(d dialect.Dialect, a *Account, now time.Time) (query string, args []any) {
	args = []any{
		a.ID, a.Scope, a.Name, a.OwnerUserID, a.BillingStatus.String(),
		a.SubscriptionPlanID, a.PaymentProcessorCustomerID, a.LastPaymentProviderSyncedAt,
		a.BillingAddress.Line1, a.BillingAddress.Line2, a.BillingAddress.City,
		a.BillingAddress.State, a.BillingAddress.PostalCode, a.BillingAddress.Country,
		a.BillingAddress.Phone, a.TimeZone, now,
	}

	return fmt.Sprintf(
		"INSERT INTO %s (id, scope, name, owner_user_id, billing_status, "+
			"subscription_plan_id, payment_processor_customer_id, last_payment_provider_synced_at, "+
			"address_line1, address_line2, address_city, address_state, "+
			"address_postal_code, address_country, address_phone, time_zone, created_at) VALUES (%s)",
		t.accounts, d.Placeholders(1, len(args)),
	), args
}

// buildSelectAccount renders the single-account read, within one scope.
func (t *tables) buildSelectAccount(d dialect.Dialect, scope tenancy.Scope, accountID string) (query string, args []any) {
	return fmt.Sprintf(
		"SELECT %s FROM %s WHERE id = %s AND scope = %s",
		accountColumns, t.accounts, d.Placeholder(1), d.Placeholder(2),
	), []any{accountID, scope}
}

// buildListAccounts renders the scope's account page, cursor-paginated on id.
func (t *tables) buildListAccounts(d dialect.Dialect, scope tenancy.Scope, cursor string, limit int) (query string, args []any) {
	args = []any{scope}

	where := "scope = " + d.Placeholder(1) + " AND archived_at IS NULL"
	if cursor != "" {
		args = append(args, cursor)
		where += " AND id > " + d.Placeholder(len(args))
	}

	args = append(args, limit)

	return fmt.Sprintf(
		"SELECT %s FROM %s WHERE %s ORDER BY id LIMIT %s",
		accountColumns, t.accounts, where, d.Placeholder(len(args)),
	), args
}

// buildCountAccounts counts the scope's live accounts.
func (t *tables) buildCountAccounts(d dialect.Dialect, scope tenancy.Scope) (query string, args []any) {
	return fmt.Sprintf(
		"SELECT COUNT(*) FROM %s WHERE scope = %s AND archived_at IS NULL",
		t.accounts, d.Placeholder(1),
	), []any{scope}
}

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
		prefixColumns("a.", accountColumns), t.accounts, t.memberships,
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

// buildUpdateAccount writes the name and address, and neither the billing state
// nor the owner — see Store.UpdateAccount.
func (t *tables) buildUpdateAccount(d dialect.Dialect, a *Account, now time.Time) (query string, args []any) {
	b := newBinder(d)

	return fmt.Sprintf(
		"UPDATE %s SET name = %s, address_line1 = %s, address_line2 = %s, address_city = %s, "+
			"address_state = %s, address_postal_code = %s, address_country = %s, address_phone = %s, "+
			"time_zone = %s, last_updated_at = %s WHERE id = %s AND scope = %s AND archived_at IS NULL",
		t.accounts, b.bind(a.Name),
		b.bind(a.BillingAddress.Line1), b.bind(a.BillingAddress.Line2), b.bind(a.BillingAddress.City),
		b.bind(a.BillingAddress.State), b.bind(a.BillingAddress.PostalCode), b.bind(a.BillingAddress.Country),
		b.bind(a.BillingAddress.Phone), b.bind(a.TimeZone),
		b.bind(now), b.bind(a.ID), b.bind(a.Scope),
	), b.args
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

// buildTransferAccountOwnership names the current owner in the predicate as well
// as the new one in the SET, so two concurrent transfers cannot both succeed and
// leave the account owned by whichever committed last.
func (t *tables) buildTransferAccountOwnership(d dialect.Dialect, scope tenancy.Scope, accountID, currentOwnerID, newOwnerID string, now time.Time) (query string, args []any) {
	b := newBinder(d)

	return fmt.Sprintf(
		"UPDATE %s SET owner_user_id = %s, last_updated_at = %s "+
			"WHERE id = %s AND scope = %s AND owner_user_id = %s AND archived_at IS NULL",
		t.accounts, b.bind(newOwnerID), b.bind(now), b.bind(accountID),
		b.bind(scope), b.bind(currentOwnerID),
	), b.args
}

// buildArchiveAccount soft-deletes an account.
func (t *tables) buildArchiveAccount(d dialect.Dialect, scope tenancy.Scope, accountID string, now time.Time) (query string, args []any) {
	b := newBinder(d)

	return fmt.Sprintf(
		"UPDATE %s SET archived_at = %s, last_updated_at = %s WHERE id = %s AND scope = %s AND archived_at IS NULL",
		t.accounts, b.bind(now), b.bind(now), b.bind(accountID), b.bind(scope),
	), b.args
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

// buildSelectMembershipID reads back the ID a revived membership kept, which is
// the row the roles have to be written against — the ID the caller generated is
// not the one in the table when the upsert took the conflict branch.
func (t *tables) buildSelectMembershipID(d dialect.Dialect, userID, accountID string) (query string, args []any) {
	return fmt.Sprintf(
		"SELECT id FROM %s WHERE belongs_to_user = %s AND belongs_to_account = %s",
		t.memberships, d.Placeholder(1), d.Placeholder(2),
	), []any{userID, accountID}
}

// buildSelectMembership reads the live membership between a user and an account.
func (t *tables) buildSelectMembership(d dialect.Dialect, scope tenancy.Scope, userID, accountID string) (query string, args []any) {
	return fmt.Sprintf(
		"SELECT %s FROM %s WHERE scope = %s AND belongs_to_user = %s AND belongs_to_account = %s "+
			"AND archived_at IS NULL",
		membershipColumns, t.memberships, d.Placeholder(1), d.Placeholder(2), d.Placeholder(3),
	), []any{scope, userID, accountID}
}

// buildListMembershipsForUser reads every live membership a user holds, default
// account first — so a caller that takes the first row gets the one the user
// lands in.
func (t *tables) buildListMembershipsForUser(d dialect.Dialect, scope tenancy.Scope, userID string) (query string, args []any) {
	return fmt.Sprintf(
		"SELECT %s FROM %s WHERE scope = %s AND belongs_to_user = %s AND archived_at IS NULL "+
			"ORDER BY default_account DESC, belongs_to_account",
		membershipColumns, t.memberships, d.Placeholder(1), d.Placeholder(2),
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
		prefixColumns("m.", membershipColumns), prefixColumns("u.", userColumns),
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

// buildSelectFallbackAccountID finds another live membership for a user, for
// moving the default off one that is being removed — so a user is never left
// with memberships and nowhere to land.
func (t *tables) buildSelectFallbackAccountID(d dialect.Dialect, scope tenancy.Scope, userID, exceptAccountID string) (query string, args []any) {
	return fmt.Sprintf(
		"SELECT belongs_to_account FROM %s WHERE scope = %s AND belongs_to_user = %s "+
			"AND belongs_to_account <> %s AND archived_at IS NULL ORDER BY belongs_to_account LIMIT 1",
		t.memberships, d.Placeholder(1), d.Placeholder(2), d.Placeholder(3),
	), []any{scope, userID, exceptAccountID}
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

// buildInsertInvitation renders the invitation write.
func (t *tables) buildInsertInvitation(d dialect.Dialect, i *Invitation, now time.Time) (query string, args []any) {
	args = []any{
		i.ID, i.Scope, i.BelongsToAccount, i.FromUser, i.ToEmail, i.ToName,
		i.ToUser, i.Token, i.Status.String(), i.Note, i.ExpiresAt.UTC(), now,
	}

	return fmt.Sprintf(
		"INSERT INTO %s (id, scope, belongs_to_account, from_user, to_email, to_name, "+
			"to_user, token, status, note, expires_at, created_at) VALUES (%s)",
		t.invitations, d.Placeholders(1, len(args)),
	), args
}

// buildSelectInvitation renders the single-invitation read, within one scope.
//
// There is no read by token: GetInvitationByToken names the row by ID and
// compares the token in Go, so the secret is never an index key and a miss
// costs the same as a hit.
func (t *tables) buildSelectInvitation(d dialect.Dialect, scope tenancy.Scope, invitationID string) (query string, args []any) {
	return fmt.Sprintf(
		"SELECT %s FROM %s WHERE id = %s AND scope = %s",
		invitationColumns, t.invitations, d.Placeholder(1), d.Placeholder(2),
	), []any{invitationID, scope}
}

// buildListInvitationsBy pages the pending invitations on one side of the
// relationship: sent by a user, or addressed to an email address. The column is
// this package's own constant, never a caller's string.
func (t *tables) buildListInvitationsBy(d dialect.Dialect, column string, scope tenancy.Scope, value, cursor string, limit int) (query string, args []any) {
	args = []any{scope, value}

	where := fmt.Sprintf(
		"scope = %s AND %s = %s AND status = 'pending' AND archived_at IS NULL",
		d.Placeholder(1), column, d.Placeholder(2),
	)

	if cursor != "" {
		args = append(args, cursor)
		where += " AND id > " + d.Placeholder(len(args))
	}

	args = append(args, limit)

	return fmt.Sprintf(
		"SELECT %s FROM %s WHERE %s ORDER BY id LIMIT %s",
		invitationColumns, t.invitations, where, d.Placeholder(len(args)),
	), args
}

// buildCountInvitationsBy counts what buildListInvitationsBy pages over.
func (t *tables) buildCountInvitationsBy(d dialect.Dialect, column string, scope tenancy.Scope, value string) (query string, args []any) {
	return fmt.Sprintf(
		"SELECT COUNT(*) FROM %s WHERE scope = %s AND %s = %s AND status = 'pending' AND archived_at IS NULL",
		t.invitations, d.Placeholder(1), column, d.Placeholder(2),
	), []any{scope, value}
}

// buildAnswerInvitation moves a pending invitation to a terminal status.
//
// The predicate requires it to still be pending, which is what makes two
// concurrent answers write once: the second matches nothing and reports zero
// rows, rather than overwriting an acceptance with a rejection.
func (t *tables) buildAnswerInvitation(d dialect.Dialect, scope tenancy.Scope, invitationID string, status InvitationStatus, note string, toUser *string, now time.Time) (query string, args []any) {
	b := newBinder(d)

	return fmt.Sprintf(
		"UPDATE %s SET status = %s, note = %s, to_user = %s, last_updated_at = %s "+
			"WHERE id = %s AND scope = %s AND status = 'pending' AND archived_at IS NULL",
		t.invitations, b.bind(status.String()), b.bind(note), b.bind(toUser),
		b.bind(now), b.bind(invitationID), b.bind(scope),
	), b.args
}

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
		userColumns, t.users, placeholders, d.Placeholder(len(args)),
	), args
}
