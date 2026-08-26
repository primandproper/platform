package identity

import (
	"database/sql"
	"time"

	"github.com/primandproper/platform-go/v13/database"
)

// The scan side of the column lists in identity/internal/queries.
//
// Each entity has an "aux" type holding the nullable columns the domain type
// carries as pointers, a targets method producing the destinations for one
// Scan, and an apply method converting them onto the value. The split is what
// lets the roster join scan a membership and a user in a single Scan call
// without a second copy of either column list — a copy that would drift from
// this one silently, since a mismatched projection is a runtime scan error
// rather than a compile error.

// pageRow is one row of a rendered list query: the value, and the two counts
// the statement carries beside it.
//
// The counts ride on the rows rather than arriving from a second query, which
// is what makes a page and the number describing it come from one snapshot of
// the table. It also means a page with no rows carries no counts — see
// filtering.Drain, which is what reports that as unknown rather than as zero.
type pageRow[T any] struct {
	value    *T
	filtered int64
	total    int64
}

// countingScanner appends the two count destinations to whatever projection the
// scanner it wraps asks for.
//
// This is what lets a page be scanned by the single-row scanner rather than by
// a second one that repeats its column list. A second scanner would be the
// exact mistake this file's header describes: a projection paired to a Scan by
// eye, where a mismatch is a runtime error rather than a compile error, and the
// page read is the one that has thirty chances per request to hit it.
type countingScanner struct {
	inner    database.Scanner
	filtered int64
	total    int64
}

func (c *countingScanner) Scan(dest ...any) error {
	return c.inner.Scan(append(dest, &c.filtered, &c.total)...)
}

// scanPage adapts a single-row scanner into one that reads a list query's row,
// counts included.
func scanPage[T any](scan func(database.Scanner) (*T, error)) func(database.Scanner) (pageRow[T], error) {
	return func(scanner database.Scanner) (pageRow[T], error) {
		counting := &countingScanner{inner: scanner}

		value, err := scan(counting)
		if err != nil {
			return pageRow[T]{}, err
		}

		return pageRow[T]{value: value, filtered: counting.filtered, total: counting.total}, nil
	}
}

// pageCounts reads the counts off a row, for filtering.Drain.
func pageCounts[T any](row pageRow[T]) (filtered, total int64) {
	return row.filtered, row.total
}

// pageValue reads the value off a row, for filtering.Drain. The value is
// returned as it stands rather than copied, so whatever a caller did to the
// slice of pointers before draining — attaching roles, redacting — is what the
// page carries.
func pageValue[T any](row pageRow[T]) *T { return row.value }

// pageValues collects a page's values, for the passes a caller makes over them
// before draining.
func pageValues[T any](rows []pageRow[T]) []*T {
	values := make([]*T, 0, len(rows))
	for _, row := range rows {
		values = append(values, row.value)
	}

	return values
}

// timePtr turns a nullable timestamp column into the *time.Time the domain
// types carry, in UTC.
//
// The UTC conversion is not cosmetic. Postgres hands back a time in the
// session's zone, MySQL in the server's, and SQLite whatever the string parsed
// as; a caller comparing two of those, or rendering one into JSON, would get an
// answer that depends on where the row was read. Every timestamp this package
// writes is UTC, so every timestamp it returns is too.
func timePtr(nt sql.NullTime) *time.Time {
	if !nt.Valid {
		return nil
	}

	t := nt.Time.UTC()

	return &t
}

// stringPtr turns a nullable text column into a *string, distinguishing a NULL
// from a stored empty string — which for a subscription plan is the difference
// between "no plan" and a plan whose ID is blank.
func stringPtr(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}

	s := ns.String

	return &s
}

// userAux holds the user columns that the User carries as pointers or
// as a named type.
type userAux struct {
	status        string
	passwordAt    sql.NullTime
	twoFactorAt   sql.NullTime
	emailAt       sql.NullTime
	termsAt       sql.NullTime
	privacyAt     sql.NullTime
	lastUpdatedAt sql.NullTime
	archivedAt    sql.NullTime
}

// targets returns one destination per column of queries.Users.Columns, in order.
func (a *userAux) targets(u *User) []any {
	return []any{
		&u.ID, &u.Scope, &u.Username, &u.EmailAddress,
		&u.FirstName, &u.LastName,
		&u.HashedPassword, &u.RequiresPasswordChange, &a.passwordAt,
		&u.TwoFactorSecret, &a.twoFactorAt,
		&a.emailAt, &u.EmailAddressVerificationToken,
		&a.status, &u.AccountStatusExplanation,
		&a.termsAt, &a.privacyAt,
		&u.CreatedAt, &a.lastUpdatedAt, &a.archivedAt,
	}
}

// apply converts what targets scanned onto the User.
func (a *userAux) apply(u *User) {
	u.CreatedAt = u.CreatedAt.UTC()
	u.AccountStatus = AccountStatus(a.status)
	u.PasswordLastChangedAt = timePtr(a.passwordAt)
	u.TwoFactorSecretVerifiedAt = timePtr(a.twoFactorAt)
	u.EmailAddressVerifiedAt = timePtr(a.emailAt)
	u.LastAcceptedTermsOfService = timePtr(a.termsAt)
	u.LastAcceptedPrivacyPolicy = timePtr(a.privacyAt)
	u.LastUpdatedAt = timePtr(a.lastUpdatedAt)
	u.ArchivedAt = timePtr(a.archivedAt)
}

// scanUser projects one row of queries.Users.Columns.
func scanUser(scanner database.Scanner) (*User, error) {
	var (
		user User
		aux  userAux
	)

	if err := scanner.Scan(aux.targets(&user)...); err != nil {
		return nil, err
	}

	aux.apply(&user)

	return &user, nil
}

// accountAux holds the nullable account columns.
type accountAux struct {
	syncedAt      sql.NullTime
	lastUpdatedAt sql.NullTime
	archivedAt    sql.NullTime
	status        string
	planID        sql.NullString
}

// targets returns one destination per column of queries.Accounts.Columns, in order.
func (a *accountAux) targets(account *Account) []any {
	return []any{
		&account.ID, &account.Scope, &account.Name, &account.OwnerUserID, &a.status,
		&a.planID, &account.PaymentProcessorCustomerID, &a.syncedAt,
		&account.BillingAddress.Line1, &account.BillingAddress.Line2,
		&account.BillingAddress.City, &account.BillingAddress.State,
		&account.BillingAddress.PostalCode, &account.BillingAddress.Country,
		&account.BillingAddress.Phone, &account.TimeZone,
		&account.CreatedAt, &a.lastUpdatedAt, &a.archivedAt,
	}
}

// apply converts what targets scanned onto the Account.
func (a *accountAux) apply(account *Account) {
	account.CreatedAt = account.CreatedAt.UTC()
	account.BillingStatus = BillingStatus(a.status)
	account.SubscriptionPlanID = stringPtr(a.planID)
	account.LastPaymentProviderSyncedAt = timePtr(a.syncedAt)
	account.LastUpdatedAt = timePtr(a.lastUpdatedAt)
	account.ArchivedAt = timePtr(a.archivedAt)
}

// scanAccount projects one row of queries.Accounts.Columns.
func scanAccount(scanner database.Scanner) (*Account, error) {
	var (
		account Account
		aux     accountAux
	)

	if err := scanner.Scan(aux.targets(&account)...); err != nil {
		return nil, err
	}

	aux.apply(&account)

	return &account, nil
}

// membershipAux holds the nullable membership columns.
type membershipAux struct {
	lastUpdatedAt sql.NullTime
	archivedAt    sql.NullTime
}

// targets returns one destination per column of queries.Memberships.Columns, in order.
func (a *membershipAux) targets(m *Membership) []any {
	return []any{
		&m.ID, &m.Scope,
		&m.BelongsToUser, &m.BelongsToAccount, &m.DefaultAccount,
		&m.CreatedAt, &a.lastUpdatedAt, &a.archivedAt,
	}
}

// apply converts what targets scanned onto the Membership.
func (a *membershipAux) apply(m *Membership) {
	m.CreatedAt = m.CreatedAt.UTC()
	m.LastUpdatedAt = timePtr(a.lastUpdatedAt)
	m.ArchivedAt = timePtr(a.archivedAt)
}

// scanMembership projects one row of queries.Memberships.Columns. Roles are not in the
// projection — they live in their own table and are read for a whole page at
// once.
func scanMembership(scanner database.Scanner) (*Membership, error) {
	var (
		membership Membership
		aux        membershipAux
	)

	if err := scanner.Scan(aux.targets(&membership)...); err != nil {
		return nil, err
	}

	aux.apply(&membership)

	return &membership, nil
}

// scanMembershipWithUser projects the roster join — the membership columns
// then the user columns — in one Scan, and redacts the user before it leaves.
//
// The redaction is here rather than at the call site because this is the value a
// roster page is made of, and a roster is the read most likely to reach a
// response body: thirty members is thirty chances to serve a password hash.
func scanMembershipWithUser(scanner database.Scanner) (*MembershipWithUser, error) {
	var (
		membership Membership
		user       User
		mAux       membershipAux
		uAux       userAux
	)

	if err := scanner.Scan(append(mAux.targets(&membership), uAux.targets(&user)...)...); err != nil {
		return nil, err
	}

	mAux.apply(&membership)
	uAux.apply(&user)

	return &MembershipWithUser{Membership: membership, User: user.Redacted()}, nil
}

// invitationAux holds the nullable invitation columns.
type invitationAux struct {
	lastUpdatedAt sql.NullTime
	archivedAt    sql.NullTime
	status        string
	toUser        sql.NullString
}

// targets returns one destination per column of queries.Invitations.Columns, in order.
func (a *invitationAux) targets(i *Invitation) []any {
	return []any{
		&i.ID, &i.Scope, &i.BelongsToAccount,
		&i.FromUser, &i.ToEmail, &i.ToName, &a.toUser,
		&i.Token, &a.status, &i.Note,
		&i.ExpiresAt, &i.CreatedAt, &a.lastUpdatedAt, &a.archivedAt,
	}
}

// apply converts what targets scanned onto the Invitation.
func (a *invitationAux) apply(i *Invitation) {
	i.ExpiresAt = i.ExpiresAt.UTC()
	i.CreatedAt = i.CreatedAt.UTC()
	i.Status = InvitationStatus(a.status)
	i.ToUser = stringPtr(a.toUser)
	i.LastUpdatedAt = timePtr(a.lastUpdatedAt)
	i.ArchivedAt = timePtr(a.archivedAt)
}

// scanInvitation projects one row of queries.Invitations.Columns.
func scanInvitation(scanner database.Scanner) (*Invitation, error) {
	var (
		invitation Invitation
		aux        invitationAux
	)

	if err := scanner.Scan(aux.targets(&invitation)...); err != nil {
		return nil, err
	}

	aux.apply(&invitation)

	return &invitation, nil
}
