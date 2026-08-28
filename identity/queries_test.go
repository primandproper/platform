package identity

import (
	"strings"
	"testing"
	"time"

	"github.com/primandproper/platform-go/v13/database/dialect"
	"github.com/primandproper/platform-go/v13/identity/internal/queries"
	"github.com/primandproper/platform-go/v13/tenancy"

	"github.com/shoenig/test"
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
//
// What it covers is now the statements querygen does not emit. The conventional
// ones number their placeholders over the finished text rather than by hand and
// are pinned by TestStatements_PlaceholdersMatchArguments instead — the same
// property, checked against the renderer's own account of it.
func TestBinder_NeverReusesAPlaceholder(T *testing.T) {
	T.Parallel()

	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	scope := tenancy.Of("dir")

	for _, d := range allDialects {
		t := newTables("")

		rendered := map[string]func() (string, []any){
			"selectUsersByIDs": func() (string, []any) { return t.buildSelectUsersByIDs(d, scope, []string{"u1", "u2"}) },
			"recordAgreementsOne": func() (string, []any) {
				return t.buildRecordAgreements(d, scope, "u1", []Agreement{TermsOfService}, now)
			},
			"recordAgreementsBoth": func() (string, []any) {
				return t.buildRecordAgreements(d, scope, "u1", []Agreement{TermsOfService, PrivacyPolicy}, now)
			},
			"eraseUser": func() (string, []any) { return t.buildEraseUser(d, scope, "u1") },

			"countLiveMemberships": func() (string, []any) { return t.buildCountLiveMembershipsForUser(d, scope, "u1") },
			"clearDefaultAccount":  func() (string, []any) { return t.buildClearDefaultAccount(d, scope, "u1", "a1", now) },
			"setDefaultAccount":    func() (string, []any) { return t.buildSetDefaultAccount(d, scope, "u1", "a1", now) },
			"archiveMembership":    func() (string, []any) { return t.buildArchiveMembership(d, scope, "u1", "a1", now) },
			"archiveMembershipsBy": func() (string, []any) { return t.buildArchiveMembershipsBy(d, membershipUserColumn, scope, "u1", now) },

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

// TestProjections_MatchTheColumnLists pins the one property the hand-written
// reads below still depend on: their SELECT list is the column list the scan
// targets are written from, in order.
func TestProjections_MatchTheColumnLists(t *testing.T) {
	t.Parallel()

	test.EqOp(t, strings.Join(queries.Users.Columns, ", "), userProjection)
	test.EqOp(t, strings.Join(queries.Accounts.Columns, ", "), accountProjection)
	test.EqOp(t, strings.Join(queries.Memberships.Columns, ", "), membershipProjection)
	test.EqOp(t, strings.Join(queries.Invitations.Columns, ", "), invitationProjection)
}
