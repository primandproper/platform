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
// Scan, and an apply method converting them onto the value.
//
// What is left here is the scan side of the statements querygen does not render
// — the sign-in reads, the batch read by id, the membership reads keyed on the
// (user, account) pair. Everything the generated package answers converts
// through identity/rows.go instead, where a renamed column is a compile error
// rather than a scan that lands one column to the left.

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
// types carry — utcPtr's normalization, for the nullable-column input shape;
// utcPtr carries the rationale.
func timePtr(nt sql.NullTime) *time.Time {
	if !nt.Valid {
		return nil
	}

	return utcPtr(&nt.Time)
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
