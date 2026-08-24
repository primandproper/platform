package identity

import (
	"database/sql"
	"time"

	"github.com/primandproper/platform-go/v13/database"
)

// The scan side of the projections in queries.go.
//
// Each entity has an "aux" type holding the nullable columns the domain type
// carries as pointers, a targets method producing the destinations for one
// Scan, and an apply method converting them onto the value. The split is what
// lets the roster join scan a membership and a user in a single Scan call
// without a second copy of either column list — a copy that would drift from
// this one silently, since a mismatched projection is a runtime scan error
// rather than a compile error.

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

// userAux holds the columns of userColumns that the User carries as pointers or
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

// targets returns one destination per column of userColumns, in order.
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

// scanUser projects one row of userColumns.
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

// accountAux holds the nullable columns of accountColumns.
type accountAux struct {
	syncedAt      sql.NullTime
	lastUpdatedAt sql.NullTime
	archivedAt    sql.NullTime
	status        string
	planID        sql.NullString
}

// targets returns one destination per column of accountColumns, in order.
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

// scanAccount projects one row of accountColumns.
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

// membershipAux holds the nullable columns of membershipColumns.
type membershipAux struct {
	lastUpdatedAt sql.NullTime
	archivedAt    sql.NullTime
}

// targets returns one destination per column of membershipColumns, in order.
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

// scanMembership projects one row of membershipColumns. Roles are not in the
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

// scanMembershipWithUser projects the roster join — membershipColumns then
// userColumns — in one Scan, and redacts the user before it leaves.
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

// invitationAux holds the nullable columns of invitationColumns.
type invitationAux struct {
	lastUpdatedAt sql.NullTime
	archivedAt    sql.NullTime
	status        string
	toUser        sql.NullString
}

// targets returns one destination per column of invitationColumns, in order.
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

// scanInvitation projects one row of invitationColumns.
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
