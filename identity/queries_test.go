package identity

import (
	"strings"
	"testing"
	"time"

	"github.com/primandproper/platform-go/v13/database/dialect"
	"github.com/primandproper/platform-go/v13/tenancy"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// allDialects is what every rendering assertion runs against, because the
// interesting failures are the ones that are correct on two of the three.
var allDialects = []dialect.Dialect{dialect.Postgres, dialect.MySQL, dialect.SQLite}

func TestTables_Naming(t *testing.T) {
	t.Parallel()

	bare := newTables("")
	test.EqOp(t, "identity_users", bare.users)
	test.EqOp(t, "identity_user_roles", bare.userRoles)
	test.EqOp(t, "identity_accounts", bare.accounts)
	test.EqOp(t, "identity_memberships", bare.memberships)
	test.EqOp(t, "identity_membership_roles", bare.membershipRoles)
	test.EqOp(t, "identity_invitations", bare.invitations)
	test.EqOp(t, "identity_invitation_roles", bare.invitationRoles)
	test.EqOp(t, "", bare.prefix())

	// The identity_ segment is the schema's, not the caller's: a table always
	// says which package created it.
	prefixed := newTables("ddb")
	test.EqOp(t, "ddb_identity_users", prefixed.users)
	test.EqOp(t, "ddb", prefixed.prefix())
}

// TestBinder_NeverReusesAPlaceholder is the regression guard for the mistake
// that is correct on Postgres and wrong on the other two.
//
// Postgres names its arguments, so `$1` written twice binds one value twice;
// SQLite and MySQL are positional, so a second `?` consumes a second argument
// and everything after it binds to the wrong column. Every statement this
// package renders must therefore have exactly as many placeholders as
// arguments.
func TestBinder_NeverReusesAPlaceholder(T *testing.T) {
	T.Parallel()

	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	scope := tenancy.Of("dir")

	user := &User{
		ID: "u1", Scope: scope, Username: "ada", EmailAddress: "ada@example.com",
		HashedPassword: "argon2$x", AccountStatus: StatusGood,
	}
	account := &Account{
		ID: "a1", Scope: scope, Name: "Acme", OwnerUserID: "u1", BillingStatus: BillingUnpaid,
	}
	membership := &Membership{
		ID: "m1", Scope: scope, BelongsToUser: "u1", BelongsToAccount: "a1", Roles: []string{"r"},
	}
	invitation := &Invitation{
		ID: "i1", Scope: scope, BelongsToAccount: "a1", FromUser: "u1",
		ToEmail: "brian@example.com", Token: "tok", Status: InvitationPending, ExpiresAt: now,
	}

	for _, d := range allDialects {
		t := newTables("")

		rendered := map[string]func() (string, []any){
			"insertUser":               func() (string, []any) { return t.buildInsertUser(d, user, now) },
			"selectUser":               func() (string, []any) { return t.buildSelectUser(d, scope, "u1") },
			"selectLiveUserBy":         func() (string, []any) { return t.buildSelectLiveUserBy(d, usernameColumn, scope, "ada") },
			"listUsers":                func() (string, []any) { return t.buildListUsers(d, scope, "cursor", 10) },
			"listUsersNoCursor":        func() (string, []any) { return t.buildListUsers(d, scope, "", 10) },
			"countUsers":               func() (string, []any) { return t.buildCountUsers(d, scope) },
			"searchUsers":              func() (string, []any) { return t.buildSearchUsers(d, scope, "ad", "cursor", 10) },
			"countSearchUsers":         func() (string, []any) { return t.buildCountSearchUsers(d, scope, "ad") },
			"selectUsersByIDs":         func() (string, []any) { return t.buildSelectUsersByIDs(d, scope, []string{"u1", "u2"}) },
			"selectUserIDByField":      func() (string, []any) { return t.buildSelectUserIDByField(d, usernameColumn, scope, "ada", "u1") },
			"selectUserIDByFieldNoExc": func() (string, []any) { return t.buildSelectUserIDByField(d, usernameColumn, scope, "ada", "") },
			"updateUser":               func() (string, []any) { return t.buildUpdateUser(d, user, now) },
			"updateUserPassword":       func() (string, []any) { return t.buildUpdateUserPassword(d, scope, "u1", "h", now) },
			"setUserFlag": func() (string, []any) {
				return t.buildSetUserFlag(d, "requires_password_change", scope, "u1", true, now)
			},
			"updateTwoFactorSecret": func() (string, []any) { return t.buildUpdateTwoFactorSecret(d, scope, "u1", "s", now) },
			"markTwoFactorVerified": func() (string, []any) { return t.buildMarkTwoFactorVerified(d, scope, "u1", now) },
			"setEmailToken":         func() (string, []any) { return t.buildSetEmailVerificationToken(d, scope, "u1", "tok", now) },
			"markEmailVerified":     func() (string, []any) { return t.buildMarkEmailVerified(d, scope, "u1", "tok", now) },
			"updateAccountStatus":   func() (string, []any) { return t.buildUpdateAccountStatus(d, scope, "u1", StatusBanned, "why", now) },
			"recordAgreementsOne": func() (string, []any) {
				return t.buildRecordAgreements(d, scope, "u1", []Agreement{TermsOfService}, now)
			},
			"recordAgreementsBoth": func() (string, []any) {
				return t.buildRecordAgreements(d, scope, "u1", []Agreement{TermsOfService, PrivacyPolicy}, now)
			},
			"archiveUser": func() (string, []any) { return t.buildArchiveUser(d, scope, "u1", now) },
			"eraseUser":   func() (string, []any) { return t.buildEraseUser(d, scope, "u1") },

			"insertAccount":        func() (string, []any) { return t.buildInsertAccount(d, account, now) },
			"selectAccount":        func() (string, []any) { return t.buildSelectAccount(d, scope, "a1") },
			"listAccounts":         func() (string, []any) { return t.buildListAccounts(d, scope, "cursor", 10) },
			"countAccounts":        func() (string, []any) { return t.buildCountAccounts(d, scope) },
			"listAccountsForUser":  func() (string, []any) { return t.buildListAccountsForUser(d, scope, "u1", "cursor", 10) },
			"countAccountsForUser": func() (string, []any) { return t.buildCountAccountsForUser(d, scope, "u1") },
			"updateAccount":        func() (string, []any) { return t.buildUpdateAccount(d, account, now) },
			"transferOwnership":    func() (string, []any) { return t.buildTransferAccountOwnership(d, scope, "a1", "u1", "u2", now) },
			"archiveAccount":       func() (string, []any) { return t.buildArchiveAccount(d, scope, "a1", now) },

			"upsertMembership":     func() (string, []any) { return t.buildUpsertMembership(d, membership, now) },
			"selectMembershipID":   func() (string, []any) { return t.buildSelectMembershipID(d, "u1", "a1") },
			"selectMembership":     func() (string, []any) { return t.buildSelectMembership(d, scope, "u1", "a1") },
			"listMemberships":      func() (string, []any) { return t.buildListMembershipsForUser(d, scope, "u1") },
			"countLiveMemberships": func() (string, []any) { return t.buildCountLiveMembershipsForUser(d, scope, "u1") },
			"listAccountMembers":   func() (string, []any) { return t.buildListAccountMembers(d, scope, "a1", "cursor", 10) },
			"countAccountMembers":  func() (string, []any) { return t.buildCountAccountMembers(d, scope, "a1") },
			"clearDefaultAccount":  func() (string, []any) { return t.buildClearDefaultAccount(d, scope, "u1", "a1", now) },
			"setDefaultAccount":    func() (string, []any) { return t.buildSetDefaultAccount(d, scope, "u1", "a1", now) },
			"selectFallback":       func() (string, []any) { return t.buildSelectFallbackAccountID(d, scope, "u1", "a1") },
			"archiveMembership":    func() (string, []any) { return t.buildArchiveMembership(d, scope, "u1", "a1", now) },
			"archiveMembershipsBy": func() (string, []any) { return t.buildArchiveMembershipsBy(d, membershipUserColumn, scope, "u1", now) },

			"insertInvitation": func() (string, []any) { return t.buildInsertInvitation(d, invitation, now) },
			"selectInvitation": func() (string, []any) { return t.buildSelectInvitation(d, scope, "i1") },
			"listInvitationsBy": func() (string, []any) {
				return t.buildListInvitationsBy(d, invitationToEmailColumn, scope, "b@x.com", "cursor", 10)
			},
			"countInvitationsBy": func() (string, []any) { return t.buildCountInvitationsBy(d, invitationToEmailColumn, scope, "b@x.com") },
			"answerInvitation": func() (string, []any) {
				return t.buildAnswerInvitation(d, scope, "i1", InvitationRejected, "no", nil, now)
			},
			"answerInvitationUser": func() (string, []any) {
				return t.buildAnswerInvitation(d, scope, "i1", InvitationAccepted, "", new("u2"), now)
			},

			"deleteRoles": func() (string, []any) { return buildDeleteRoles(d, t.membershipRoles, membershipIDColumn, "m1") },
			"insertRoles": func() (string, []any) {
				return buildInsertRoles(d, t.membershipRoles, membershipIDColumn, "m1", []string{"a", "b"})
			},
			"selectRoles": func() (string, []any) {
				return buildSelectRoles(d, t.membershipRoles, membershipIDColumn, []string{"m1", "m2"})
			},
		}

		for name, build := range rendered {
			T.Run(string(d)+"/"+name, func(t *testing.T) {
				t.Parallel()

				query, args := build()

				test.EqOp(t, len(args), countPlaceholders(query, d),
					test.Sprintf("query: %s", query))

				// A statement that reached the driver with an unrendered table
				// name would fail there rather than here.
				test.False(t, strings.Contains(query, "{{"))
			})
		}
	}
}

// countPlaceholders counts the argument slots a statement actually has.
//
// Positional dialects are a count of '?'. Postgres is a count of distinct $n,
// which is what makes reuse legal there — and the assertion above then reads as
// "no argument is bound and never referenced" on Postgres and as "the counts
// match" on the other two, which is the right check for each.
func countPlaceholders(query string, d dialect.Dialect) int {
	if d != dialect.Postgres {
		return strings.Count(query, "?")
	}

	seen := map[string]struct{}{}

	for i := 0; i < len(query); i++ {
		if query[i] != '$' {
			continue
		}

		j := i + 1
		for j < len(query) && query[j] >= '0' && query[j] <= '9' {
			j++
		}

		if j > i+1 {
			seen[query[i:j]] = struct{}{}
		}
	}

	return len(seen)
}

//go:fix inline
func ptr[T any](v T) *T { return new(v) }

func TestLikePrefix(t *testing.T) {
	t.Parallel()

	// Without the escaping, a search for "a%" matches the whole directory —
	// which reads as a working search returning too much rather than as a bug.
	test.EqOp(t, `ad%`, likePrefix("ad"))
	test.EqOp(t, `a!%%`, likePrefix("a%"))
	test.EqOp(t, `a!_%`, likePrefix("a_"))
	test.EqOp(t, `a!!%`, likePrefix("a!"))

	// A backslash is ordinary here, which is the point of not using it as the
	// escape character.
	test.EqOp(t, `a\%`, likePrefix(`a\`))

	// The escape rule must not double the escapes the wildcard rules introduce.
	test.EqOp(t, `!%!!%`, likePrefix("%!"))
}

func TestUpsertMembership_DialectSyntax(t *testing.T) {
	t.Parallel()

	tables := newTables("")
	membership := &Membership{ID: "m1", Scope: tenancy.Global(), BelongsToUser: "u1", BelongsToAccount: "a1"}
	now := time.Now().UTC()

	pg, _ := tables.buildUpsertMembership(dialect.Postgres, membership, now)
	test.StrContains(t, pg, "ON CONFLICT (belongs_to_user, belongs_to_account) DO UPDATE SET")
	test.StrContains(t, pg, "archived_at = NULL")

	my, _ := tables.buildUpsertMembership(dialect.MySQL, membership, now)
	test.StrContains(t, my, "ON DUPLICATE KEY UPDATE")

	lite, _ := tables.buildUpsertMembership(dialect.SQLite, membership, now)
	test.StrContains(t, lite, "ON CONFLICT (belongs_to_user, belongs_to_account) DO UPDATE SET")

	// created_at is not in any update clause: rejoining an account does not make
	// the relationship new.
	for _, query := range []string{pg, my, lite} {
		test.EqOp(t, 1, strings.Count(query, "created_at"))
	}
}

func TestBuildUpdateUser_AssignmentOrder(t *testing.T) {
	t.Parallel()

	// MySQL evaluates a single-table UPDATE's assignments left to right against
	// values earlier ones wrote. The verification CASE therefore has to come
	// before email_address, or a user could move to an address they have never
	// proven and stay verified — on one dialect only.
	for _, d := range allDialects {
		query, _ := newTables("").buildUpdateUser(d, &User{Scope: tenancy.Global()}, time.Now().UTC())

		caseAt := strings.Index(query, "email_address_verified_at = CASE")
		assignAt := strings.Index(query, "email_address = $")

		if d != dialect.Postgres {
			assignAt = strings.Index(query, "email_address = ?")
		}

		must.GreaterEq(t, 0, caseAt)
		must.GreaterEq(t, 0, assignAt)
		test.Less(t, assignAt, caseAt)
	}
}

func TestBuildUpdateAccountBilling_WritesOnlyWhatIsNamed(t *testing.T) {
	t.Parallel()

	tables := newTables("")
	now := time.Now().UTC()

	query, args := tables.buildUpdateAccountBilling(dialect.Postgres, tenancy.Global(), "a1", &BillingUpdate{
		Status: new(BillingPaid),
	}, now)

	// A processor webhook carrying a status alone must not read-modify-write the
	// rest and lose what a concurrent one did.
	test.StrContains(t, query, "billing_status =")
	test.StrNotContains(t, query, "subscription_plan_id =")
	test.StrNotContains(t, query, "payment_processor_customer_id =")
	test.SliceLen(t, 4, args)

	full, fullArgs := tables.buildUpdateAccountBilling(dialect.Postgres, tenancy.Global(), "a1", &BillingUpdate{
		Status:                     new(BillingTrial),
		SubscriptionPlanID:         new("plan"),
		PaymentProcessorCustomerID: new("cus"),
		SyncedAt:                   &now,
	}, now)
	test.StrContains(t, full, "subscription_plan_id =")
	test.SliceLen(t, 7, fullArgs)
}

func TestNullableString(t *testing.T) {
	t.Parallel()

	// A cancelled subscription and one whose ID is blank are different facts.
	test.Nil(t, nullableString(""))
	test.EqOp(t, "plan", nullableString("plan"))
}

func TestPrefixColumns(t *testing.T) {
	t.Parallel()

	test.EqOp(t, "e.id, e.scope", prefixColumns("e.", "id, scope"))
}
