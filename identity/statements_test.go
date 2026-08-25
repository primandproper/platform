package identity

import (
	"strings"
	"testing"
	"time"

	"github.com/primandproper/platform-go/v13/database/dialect"
	"github.com/primandproper/platform-go/v13/database/querygen"
	"github.com/primandproper/platform-go/v13/filtering"
	"github.com/primandproper/platform-go/v13/identity/internal/queries"
	"github.com/primandproper/platform-go/v13/pointer"
	"github.com/primandproper/platform-go/v13/tenancy"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// renderedStatements returns every statement this store renders for d, named.
//
// It is a function rather than a slice on the type so that a statement added to
// the struct and not to this list is visible as a shorter list here rather than
// as a statement nothing covers. The count is asserted below.
func renderedStatements(s *statements) map[string]querygen.Bound {
	rendered := map[string]querygen.Bound{
		"createUser":  s.createUser,
		"getUser":     s.getUser,
		"listUsers":   s.listUsers,
		"updateUser":  s.updateUser,
		"archiveUser": s.archiveUser,

		"createAccount":  s.createAccount,
		"getAccount":     s.getAccount,
		"listAccounts":   s.listAccounts,
		"updateAccount":  s.updateAccount,
		"archiveAccount": s.archiveAccount,

		"createInvitation": s.createInvitation,
		"getInvitation":    s.getInvitation,
	}

	for column := range s.listInvitationsBy {
		rendered["listInvitationsBy/"+column] = s.listInvitationsBy[column]
	}

	return rendered
}

// TestStatements_PlaceholdersMatchArguments is the binder's guarantee, checked
// against the renderer's own account of it.
//
// Every statement here has its placeholders numbered over the finished text, so
// the count and the argument list cannot disagree by construction — but "by
// construction" is the claim, and this is what makes it a failing test rather
// than a comment if the construction changes. The failure it guards against is
// the one that is correct on Postgres and wrong on the other two: a reused
// index binds one value twice where a repeated `?` consumes a second argument
// and shifts every argument after it into the wrong column.
func TestStatements_PlaceholdersMatchArguments(T *testing.T) {
	T.Parallel()

	for _, d := range allDialects {
		rendered := renderedStatements(newStatements(d, newTables("ddb")))

		// Fourteen: twelve named above, plus one paged invitation read per
		// column the two of them key on.
		must.MapLen(T, 14, rendered)

		for name, statement := range rendered {
			T.Run(string(d)+"/"+name, func(t *testing.T) {
				t.Parallel()

				test.EqOp(t, len(statement.Args), countPlaceholders(statement.SQL, d),
					test.Sprintf("query: %s", statement.SQL))

				// A statement that reached the driver with an unrendered table
				// name would fail there rather than here.
				test.StrNotContains(t, statement.SQL, "{{")

				// The prefix is the consumer's, applied at construction. sqlc
				// resolves table names when it runs, which is why the canonical
				// .sql carries the bare name and this carries the rendered one.
				test.StrContains(t, statement.SQL, "ddb_identity_")
				test.StrNotContains(t, statement.SQL, "sqlc.")
			})
		}
	}
}

// TestStatements_BindScopeAsAScope pins the tenancy obligation at the one place
// it can be lost: the value, not the column.
//
// tenancy.Scope is a driver.Valuer whose zero value fails at Value, so binding
// the Scope itself makes an unset scope a driver error. Binding a string
// derived from one — scope.String(), or the owner identifier — makes an unset
// scope the empty string, which is tenancy.Global() and a perfectly good scope
// with rows in it.
func TestStatements_BindScopeAsAScope(T *testing.T) {
	T.Parallel()

	for _, d := range allDialects {
		T.Run(string(d), func(t *testing.T) {
			t.Parallel()

			s := newStatements(d, newTables(""))
			scope := tenancy.Of("dir")

			for name, values := range map[string]map[string]any{
				"getUser":     keyed(scope, "u1"),
				"archiveUser": keyed(scope, "u1"),
				"listUsers":   s.listValues(nil, keyedScope(scope)),
			} {
				statement := renderedStatements(s)[name]

				args, err := statement.Bind(values)
				must.NoError(t, err, must.Sprintf("statement %q", name))

				var scopes int
				for _, arg := range args {
					if _, ok := arg.(tenancy.Scope); ok {
						scopes++
					}
				}

				// One occurrence on Postgres, which numbers its markers; three
				// in a list query on the positional dialects, which bind the
				// SELECT's and both counts' separately.
				test.Greater(t, 0, scopes, test.Sprintf("statement %q bound no scope", name))
			}

			// And an unset scope reaches the driver as a failure rather than as
			// the global one.
			args, err := s.getUser.Bind(keyed(tenancy.Scope{}, "u1"))
			must.NoError(t, err)

			for _, arg := range args {
				if unset, ok := arg.(tenancy.Scope); ok {
					_, valueErr := unset.Value()
					test.ErrorIs(t, valueErr, tenancy.ErrNoScope)
				}
			}
		})
	}
}

// TestStatements_BindReportsAMissingArgument pins Bind's refusal to default.
//
// Every nullable argument here takes nil legitimately, so a name with no value
// and a name bound to nil are indistinguishable once bound — which is why the
// missing one is an error rather than a nil.
func TestStatements_BindReportsAMissingArgument(t *testing.T) {
	t.Parallel()

	s := newStatements(dialect.Postgres, newTables(""))

	_, err := bind(s.getUser, map[string]any{querygen.IDColumn: "u1"}, "reading identity user")
	must.Error(t, err)
	test.ErrorIs(t, err, querygen.ErrUnboundArgument)
	test.StrContains(t, err.Error(), queries.ScopeColumn)
}

// TestStatements_UpdatesAssignOnlyTheirOwnColumns is the guard on what a
// generated UPDATE is allowed to touch.
//
// querygen assigns every column its options leave mutable, and the caller's
// struct is often a Redacted copy whose credential fields are empty — so an
// update set that included hashed_password would blank a password hash on every
// profile save, silently and for every user who ever edited their name.
func TestStatements_UpdatesAssignOnlyTheirOwnColumns(t *testing.T) {
	t.Parallel()

	s := newStatements(dialect.Postgres, newTables(""))

	assigned := func(sql, column string) bool {
		return strings.Contains(sql, "\n\t"+column+" = ")
	}

	for _, column := range []string{"username", "email_address", "first_name", "last_name", "email_address_verified_at"} {
		test.True(t, assigned(s.updateUser.SQL, column), test.Sprintf("column %q", column))
	}

	for _, column := range []string{
		"hashed_password", "requires_password_change", "two_factor_secret",
		"two_factor_secret_verified_at", "email_address_verification_token",
		"account_status", "account_status_explanation", queries.ScopeColumn, "id", "created_at",
	} {
		test.False(t, assigned(s.updateUser.SQL, column), test.Sprintf("column %q", column))
	}

	test.True(t, assigned(s.updateAccount.SQL, "name"))
	test.True(t, assigned(s.updateAccount.SQL, "address_city"))

	for _, column := range []string{
		"owner_user_id", "billing_status", "subscription_plan_id",
		"payment_processor_customer_id", "last_payment_provider_synced_at",
	} {
		test.False(t, assigned(s.updateAccount.SQL, column), test.Sprintf("column %q", column))
	}
}

// TestStatements_CreatesOmitTheDatabaseOwnedColumns pins the other half of the
// created_at decision: the column is not in the insert, so the DEFAULT in the
// schema is what fills it.
func TestStatements_CreatesOmitTheDatabaseOwnedColumns(t *testing.T) {
	t.Parallel()

	s := newStatements(dialect.Postgres, newTables(""))

	for _, statement := range []querygen.Bound{s.createUser, s.createAccount, s.createInvitation} {
		for _, column := range []string{"created_at", "last_updated_at", "archived_at"} {
			test.StrNotContains(t, statement.SQL, "\n\t"+column, test.Sprintf("column %q", column))
		}

		// The scope is not database-owned and must be supplied, because the
		// column deliberately has no DEFAULT — see identity/migrations.
		test.StrContains(t, statement.SQL, "\n\t"+queries.ScopeColumn)
	}
}

// TestStatements_ListsCarryTheWindowAndBothCounts pins what the port bought the
// paged reads, since none of it is visible in a signature that did not change.
func TestStatements_ListsCarryTheWindowAndBothCounts(t *testing.T) {
	t.Parallel()

	s := newStatements(dialect.Postgres, newTables(""))

	for _, statement := range []querygen.Bound{s.listUsers, s.listAccounts, s.listInvitationsBy[invitationToEmailColumn]} {
		test.StrContains(t, statement.SQL, "AS filtered_count")
		test.StrContains(t, statement.SQL, "AS total_count")

		for _, argument := range []string{
			filtering.ArgCreatedAfter, filtering.ArgCreatedBefore,
			filtering.ArgUpdatedAfter, filtering.ArgUpdatedBefore,
			filtering.ArgIncludeArchived, filtering.ArgCursor, filtering.ArgResultLimit,
		} {
			test.SliceContains(t, statement.Args, argument)
		}
	}

	// The pending predicate is a bound match rather than a literal in the
	// statement text.
	pending := s.listInvitationsBy[invitationFromUserColumn]
	test.SliceContains(t, pending.Args, invitationStatusColumn)
	test.StrNotContains(t, pending.SQL, "'pending'")
	test.SliceContains(t, pending.Args, invitationFromUserColumn)
}

// TestStatements_ListValuesBindTheWholeWindow pins that a filter reaches the
// statement complete, since Bind refuses a name with no value and a window
// argument bound under the wrong name would filter nothing.
func TestStatements_ListValuesBindTheWholeWindow(T *testing.T) {
	T.Parallel()

	at := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)

	filter := &filtering.QueryFilter{
		CreatedAfter:    pointer.To(at),
		CreatedBefore:   pointer.To(at.Add(time.Hour)),
		UpdatedAfter:    pointer.To(at),
		UpdatedBefore:   pointer.To(at.Add(time.Hour)),
		IncludeArchived: pointer.To(true),
		Cursor:          pointer.To("u1"),
		MaxResponseSize: pointer.To(uint16(20)),
	}

	for _, d := range allDialects {
		T.Run(string(d), func(t *testing.T) {
			t.Parallel()

			s := newStatements(d, newTables(""))

			args, err := s.listUsers.Bind(s.listValues(filter, keyedScope(tenancy.Of("dir"))))
			must.NoError(t, err)
			test.SliceLen(t, len(s.listUsers.Args), args)

			// SQLite compares timestamps as text, in the shape its own
			// CURRENT_TIMESTAMP writes — a time.Time bound there admits every
			// row for every bound, which is indistinguishable from no filter.
			bound := s.listValues(filter, nil)[filtering.ArgCreatedAfter]
			if d == dialect.SQLite {
				test.EqOp[any](t, at.Format(time.DateTime), bound)
			} else {
				test.NotEqOp[any](t, at.Format(time.DateTime), bound)
			}
		})
	}
}
