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
		"UpdateUserPassword", "SetUserRequiresPasswordChange", "UpdateUserTwoFactorSecret",
		"SetUserEmailAddressVerificationToken", "MarkUserEmailAddressVerified",
		"UpdateUserAccountStatus", "TransferAccountOwnership", "AnswerInvitation",
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

			// Memberships is declared for its columns and emits nothing: every
			// one of its statements keys on the (user, account) pair or is an
			// upsert, and neither is a shape this generator produces.
			test.StrNotContains(t, rendered, MembershipsTable)
		})
	}
}

// TestTables_ScopeIsInEveryStatement is the tenancy obligation read off the
// emitted text: no statement omits the scope, so there is no read a caller can
// reach that answers across scopes.
func TestTables_ScopeIsInEveryStatement(T *testing.T) {
	T.Parallel()

	for _, d := range everyDialect {
		T.Run(string(d), func(t *testing.T) {
			t.Parallel()

			for statement := range strings.SplitSeq(Render(d), "-- name: ") {
				if statement == "" {
					continue
				}

				test.StrContains(t, statement, ScopeColumn,
					test.Sprintf("statement %q", strings.SplitN(statement, "\n", 2)[0]))
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

// TestFieldWrites_NameRealColumns catches the typo neither sqlc nor the
// compiler would report as one. A column name the table does not have makes an
// UPDATE sqlc rejects, which is loud — but a *guard argument* misspelled to
// match a column name silently collapses the guard onto the assignment, and the
// statement still generates, still compiles, and no longer guards anything.
func TestFieldWrites_NameRealColumns(t *testing.T) {
	t.Parallel()

	assigned := map[*Table][]string{
		&Users: {
			hashedPasswordColumn, requiresPasswordChangeColumn, passwordLastChangedAtColumn,
			twoFactorColumn, twoFactorVerifiedAtColumn,
			EmailAddressVerifiedAtColumn, emailVerificationTokenColumn,
			accountStatusColumn, accountStatusExplanationColumn,
		},
		&Accounts:    {ownerUserIDColumn},
		&Invitations: {InvitationStatusColumn, invitationNoteColumn, invitationToUserColumn},
	}

	for table, columns := range assigned {
		for _, column := range columns {
			test.True(t, slices.Contains(table.Columns, column),
				test.Sprintf("table %q does not have column %q", table.Name, column))
		}
	}

	// A guard argument is a name, not a column, and it must not be one: sharing
	// a name with the column it compares would make the write set the column to
	// the value it was requiring it to already hold.
	for _, table := range allTables {
		for _, arg := range []string{currentEmailVerificationTokenArg, currentOwnerUserIDArg, currentInvitationStatusArg} {
			test.False(t, slices.Contains(table.Columns, arg),
				test.Sprintf("guard argument %q collides with a column of %q", arg, table.Name))
		}
	}
}

// TestFieldWrites_GuardsSurvive pins the three predicates whose absence is
// silent. Each is what makes a losing writer report zero rows rather than
// overwrite the winner — a second click on a verification link, a second
// concurrent ownership transfer, a rejection landing on top of an acceptance —
// and a statement that lost its guard behaves correctly right up until two
// requests arrive together.
func TestFieldWrites_GuardsSurvive(T *testing.T) {
	T.Parallel()

	guards := map[string]string{
		"MarkUserEmailAddressVerified": emailVerificationTokenColumn + " = sqlc.arg(" + currentEmailVerificationTokenArg + ")",
		"TransferAccountOwnership":     ownerUserIDColumn + " = sqlc.arg(" + currentOwnerUserIDArg + ")",
		"AnswerInvitation":             InvitationStatusColumn + " = sqlc.arg(" + currentInvitationStatusArg + ")",
	}

	for _, d := range everyDialect {
		T.Run(string(d), func(t *testing.T) {
			t.Parallel()

			var seen []string

			for statement := range strings.SplitSeq(Render(d), "-- name: ") {
				name, _, _ := strings.Cut(statement, " ")

				guard, ok := guards[name]
				if !ok {
					continue
				}

				test.StrContains(t, statement, guard, test.Sprintf("statement %q", name))

				// The assignment is there too, under the column's own name, so
				// the two ends of the comparison are two arguments rather than
				// one — which is the whole of what makes the guard a guard.
				test.StrContains(t, statement, strings.SplitN(guard, " = ", 2)[0]+" = sqlc.",
					test.Sprintf("statement %q", name))

				seen = append(seen, name)
			}

			// Without this the loop above passes for a rendering that emits
			// none of the three, which is the failure it exists to catch.
			test.SliceLen(t, len(guards), seen)
		})
	}
}

// TestFieldWrites_StampLastUpdatedAt pins the convention half: every one of
// these writes stamps the column from the server's clock, rather than assigning
// it from a bound value or leaving it behind. A row whose last_updated_at came
// off an application clock is a row the updated_after window can exclude while
// another instance's rows of the same age survive.
func TestFieldWrites_StampLastUpdatedAt(T *testing.T) {
	T.Parallel()

	written := []string{
		"UpdateUserPassword", "SetUserRequiresPasswordChange", "UpdateUserTwoFactorSecret",
		"SetUserEmailAddressVerificationToken", "MarkUserEmailAddressVerified",
		"UpdateUserAccountStatus", "TransferAccountOwnership", "AnswerInvitation",
	}

	for _, d := range everyDialect {
		T.Run(string(d), func(t *testing.T) {
			t.Parallel()

			var seen []string

			for statement := range strings.SplitSeq(Render(d), "-- name: ") {
				name, _, _ := strings.Cut(statement, " ")

				if !slices.Contains(written, name) {
					continue
				}

				test.StrContains(t, statement, querygen.LastUpdatedAtColumn+" = "+querygen.NowExpression,
					test.Sprintf("statement %q", name))
				test.StrNotContains(t, statement, "sqlc.arg("+querygen.LastUpdatedAtColumn+")",
					test.Sprintf("statement %q", name))

				seen = append(seen, name)
			}

			test.SliceLen(t, len(written), seen)
		})
	}
}
