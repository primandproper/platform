package queries

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/primandproper/platform-go/v13/database/dialect"
	"github.com/primandproper/platform-go/v13/database/querygen"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// everyDialect is what the rendering assertions run against, because the
// interesting failures are the ones that are correct on two of the three.
var everyDialect = []dialect.Dialect{dialect.Postgres, dialect.MySQL, dialect.SQLite}

// allTables is every table declared here, emitted or not.
var allTables = []*Table{&Users, &Accounts, &Invitations, &Memberships}

// TestRender_MatchesTheCommittedFiles is the regeneration gate, run locally
// rather than only in CI.
//
// The .sql files are what sqlc is run over, and the whole value of running it is
// that they are the statements the store executes. A hand-edit to one — or a
// column list changed without regenerating — would leave sqlc checking SQL
// nobody runs, which is a green check over an unchecked store.
func TestRender_MatchesTheCommittedFiles(T *testing.T) {
	T.Parallel()

	for _, d := range everyDialect {
		T.Run(string(d), func(t *testing.T) {
			t.Parallel()

			committed, err := os.ReadFile(FileName(d))
			must.NoError(t, err)

			// The committed file carries the generated-code header, which is
			// the generator's rather than this function's.
			body := string(committed)
			if index := strings.Index(body, "-- name:"); index > 0 {
				body = body[index:]
			}

			test.EqOp(t, Render(d), body,
				test.Sprintf("run `make generate` and commit %s", FileName(d)))
		})
	}
}

// TestRender_EmitsTheStatementsTheStoreExecutes pins the set, since a query
// emitted here and not executed is SQL nobody checks the other way round: sqlc
// would be reading a statement the store does not run.
func TestRender_EmitsTheStatementsTheStoreExecutes(T *testing.T) {
	T.Parallel()

	want := []string{
		"CreateUser", "GetUser", "ListUsers", "UpdateUser", "ArchiveUser",
		"CreateAccount", "GetAccount", "ListAccounts", "UpdateAccount", "ArchiveAccount",
		"CreateInvitation", "GetInvitation", "ListInvitations",
		"ListInvitationsByFromUser", "ListInvitationsByToEmail",
		"GetUserCreatedAt", "GetAccountCreatedAt", "GetInvitationCreatedAt",
		"GetUserByUsername", "GetUserByEmailAddress", "GetUserByEmailVerificationToken",
		"GetMembershipByUserAndAccount", "GetMembershipIDByUserAndAccount",
		"GetMembershipFallbackAccountID",
	}

	for _, d := range everyDialect {
		T.Run(string(d), func(t *testing.T) {
			t.Parallel()

			rendered := Render(d)

			var names []string
			for line := range strings.SplitSeq(rendered, "\n") {
				if after, ok := strings.CutPrefix(line, "-- name: "); ok {
					names = append(names, strings.Fields(after)[0])
				}
			}

			test.SliceEqFunc(t, want, names, func(a, b string) bool { return a == b })

			// An invitation is answered rather than edited or archived, and
			// nothing here checks a row's existence without reading it.
			test.StrNotContains(t, rendered, "UpdateInvitation")
			test.StrNotContains(t, rendered, "ArchiveInvitation")
			test.StrNotContains(t, rendered, "Existence")

			// Memberships gets no standard query — every one of them would key
			// on the id the table does not address rows by — but its three
			// keyed reads are here, which is the point of the keyed forms.
			test.StrNotContains(t, rendered, "GetMemberships")
			test.StrNotContains(t, rendered, "ListMemberships")
		})
	}
}

// TestTables_ScopeIsInEveryStatement is the tenancy obligation read off the
// emitted text: no statement omits the scope, so there is no read a caller can
// reach that answers across scopes.
func TestTables_ScopeIsInEveryStatement(T *testing.T) {
	T.Parallel()

	// The three exceptions, and they are the same exception three times: the
	// read-back of the creation time a create's own INSERT just caused, by the
	// id that create minted, inside that create's transaction. It is the
	// component's own machinery servicing itself rather than a read a caller
	// reaches — the row is not visible to anything else until the transaction
	// commits — so it keys on the id alone. Everything else, without exception,
	// names the scope.
	unscoped := []string{"GetUserCreatedAt", "GetAccountCreatedAt", "GetInvitationCreatedAt"}

	for _, d := range everyDialect {
		T.Run(string(d), func(t *testing.T) {
			t.Parallel()

			for statement := range strings.SplitSeq(Render(d), "-- name: ") {
				if statement == "" {
					continue
				}

				name := strings.Fields(statement)[0]
				if slices.Contains(unscoped, name) {
					continue
				}

				test.StrContains(t, statement, ScopeColumn, test.Sprintf("statement %q", name))
			}
		})
	}
}

// TestTable_KeyedColumns pins the idiom the keyed reads depend on: the column
// list a statement's predicates are derived from, without the id.
func TestTable_KeyedColumns(t *testing.T) {
	t.Parallel()

	for _, table := range allTables {
		keyed := table.KeyedColumns()

		test.False(t, slices.Contains(keyed, querygen.IDColumn), test.Sprintf("table %q", table.Name))
		test.EqOp(t, len(table.Columns)-1, len(keyed), test.Sprintf("table %q", table.Name))

		// Everything else survives, in order — the archived column above all,
		// since it is what keeps a keyed read from returning archived rows.
		test.True(t, slices.Contains(keyed, querygen.ArchivedAtColumn), test.Sprintf("table %q", table.Name))
	}
}

// TestRender_KeyedReadsAddressARowByItsKey is the property the membership reads
// exist for: they key on the (user, account) pair, not on the id the table also
// carries, while still projecting whatever the store scans.
func TestRender_KeyedReadsAddressARowByItsKey(T *testing.T) {
	T.Parallel()

	for _, d := range everyDialect {
		T.Run(string(d), func(t *testing.T) {
			t.Parallel()

			byName := map[string]string{}
			for statement := range strings.SplitSeq(Render(d), "-- name: ") {
				if statement != "" {
					byName[strings.Fields(statement)[0]] = statement
				}
			}

			for _, name := range []string{
				"GetMembershipByUserAndAccount",
				"GetMembershipIDByUserAndAccount",
				"GetMembershipFallbackAccountID",
			} {
				statement, ok := byName[name]
				must.True(t, ok, must.Sprintf("statement %q was not rendered", name))

				// Keyed on the pair rather than on the id.
				test.StrNotContains(t, statement, "sqlc.arg("+querygen.IDColumn+")",
					test.Sprintf("statement %q", name))
				test.StrContains(t, statement, "sqlc.arg("+MembershipUserColumn+")",
					test.Sprintf("statement %q", name))
				test.StrContains(t, statement, "sqlc.arg("+MembershipAccountColumn+")",
					test.Sprintf("statement %q", name))

				// Live rows only. A keyed read is not a filtered list, and an
				// archived membership answering one is a departed member who
				// still belongs to the account.
				test.StrContains(t, statement, querygen.ArchivedAtColumn+" IS NULL",
					test.Sprintf("statement %q", name))
			}

			// The membership read projects the id it does not key on, because
			// the roles are written against it.
			test.StrContains(t, byName["GetMembershipByUserAndAccount"],
				querygen.Qualify(MembershipsTable, querygen.IDColumn))

			// The fallback excludes rather than matches, and names the order
			// that makes "another account" a row rather than whichever one the
			// planner reached first.
			test.StrContains(t, byName["GetMembershipFallbackAccountID"],
				MembershipAccountColumn+" <> sqlc.arg("+MembershipAccountColumn+")")
			test.StrContains(t, byName["GetMembershipFallbackAccountID"], "LIMIT 1")
		})
	}
}

// TestRender_KeyedUserReadsEnumerateTheColumn pins what replaced the builder
// parameterized on a column: one named statement per column, each live-only and
// each scoped.
func TestRender_KeyedUserReadsEnumerateTheColumn(T *testing.T) {
	T.Parallel()

	byColumn := map[string]string{
		"GetUserByUsername":               UserUsernameColumn,
		"GetUserByEmailAddress":           UserEmailAddressColumn,
		"GetUserByEmailVerificationToken": UserEmailVerificationTokenColumn,
	}

	for _, d := range everyDialect {
		T.Run(string(d), func(t *testing.T) {
			t.Parallel()

			statements := map[string]string{}
			for statement := range strings.SplitSeq(Render(d), "-- name: ") {
				if statement != "" {
					statements[strings.Fields(statement)[0]] = statement
				}
			}

			for name, column := range byColumn {
				statement, ok := statements[name]
				must.True(t, ok, must.Sprintf("statement %q was not rendered", name))

				test.StrContains(t, statement, "sqlc.arg("+column+")", test.Sprintf("statement %q", name))
				test.StrContains(t, statement, "sqlc.arg("+ScopeColumn+")", test.Sprintf("statement %q", name))
				test.StrContains(t, statement, querygen.ArchivedAtColumn+" IS NULL",
					test.Sprintf("statement %q", name))

				// The whole user comes back — these are the sign-in reads, and
				// the caller needs the credential columns.
				for _, projected := range Users.Columns {
					test.StrContains(t, statement, querygen.Qualify(UsersTable, projected),
						test.Sprintf("statement %q column %q", name, projected))
				}
			}
		})
	}
}

// TestTable_InsertColumns pins what a caller supplies and what the database
// fills in.
func TestTable_InsertColumns(t *testing.T) {
	t.Parallel()

	for _, table := range allTables {
		columns := table.InsertColumns()

		// The database owns these three, and created_at is the one that
		// changed: it has a DEFAULT in the schema precisely so that it can be
		// absent here. A row whose creation time disagrees with its id is a row
		// the cursor walk and the filter window order differently.
		for _, column := range []string{"created_at", "last_updated_at", "archived_at"} {
			test.False(t, slices.Contains(columns, column),
				test.Sprintf("table %q column %q", table.Name, column))
		}

		// The scope is the caller's, because the column deliberately has no
		// DEFAULT — the empty string there is tenancy.Global() rather than
		// "unset".
		test.True(t, slices.Contains(columns, ScopeColumn), test.Sprintf("table %q", table.Name))
		test.True(t, slices.Contains(columns, querygen.IDColumn), test.Sprintf("table %q", table.Name))
	}
}

// TestTable_UpdateColumns pins what the standard update is allowed to assign.
func TestTable_UpdateColumns(t *testing.T) {
	t.Parallel()

	for _, table := range allTables {
		columns := table.UpdateColumns()

		// An UPDATE that assigned the column its WHERE matches on is a row
		// changing its own identity mid-statement, and one that could reassign
		// the scope makes the scope on every other statement a formality.
		test.False(t, slices.Contains(columns, querygen.IDColumn), test.Sprintf("table %q", table.Name))
		test.False(t, slices.Contains(columns, ScopeColumn), test.Sprintf("table %q", table.Name))

		for _, immutable := range table.Immutable() {
			test.False(t, slices.Contains(columns, immutable),
				test.Sprintf("table %q column %q", table.Name, immutable))
		}
	}

	// Named rather than derived, because the set is the whole reason UpdateUser
	// cannot blank a password hash off a Redacted struct.
	test.SliceEqFunc(t,
		[]string{"username", "email_address", "first_name", "last_name", "email_address_verified_at"},
		Users.UpdateColumns(),
		func(a, b string) bool { return a == b },
	)
}

// TestTables_NullableAndUpdatableNameRealColumns catches the typo neither sqlc
// nor the compiler can: a name the table does not have is silently ignored, so a
// misspelled nullable column would bind a NULL the schema rejects and a
// misspelled updatable one would simply not be assigned.
func TestTables_NullableAndUpdatableNameRealColumns(t *testing.T) {
	t.Parallel()

	for _, table := range allTables {
		for _, column := range slices.Concat(table.Nullable, table.Updatable) {
			test.True(t, slices.Contains(table.Columns, column),
				test.Sprintf("table %q does not have column %q", table.Name, column))
		}
	}
}

// TestTables_ColumnsAreUniqueAndConventional pins the shape querygen derives
// from: an id, and the three convention columns whose presence decides which
// queries a table gets.
func TestTables_ColumnsAreUniqueAndConventional(t *testing.T) {
	t.Parallel()

	for _, table := range allTables {
		seen := map[string]struct{}{}
		for _, column := range table.Columns {
			_, duplicate := seen[column]
			test.False(t, duplicate, test.Sprintf("table %q repeats column %q", table.Name, column))
			seen[column] = struct{}{}
		}

		for _, column := range []string{querygen.IDColumn, ScopeColumn, "created_at", "last_updated_at", "archived_at"} {
			test.True(t, slices.Contains(table.Columns, column),
				test.Sprintf("table %q is missing %q", table.Name, column))
		}
	}
}
