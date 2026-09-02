package tableaccess

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"hash/fnv"
	"testing"

	"github.com/primandproper/platform-go/v14/database"
	"github.com/primandproper/platform-go/v14/testutils/containers/mysqltest"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

const defaultMySQLImage = "mariadb:11"

func reverseString(input string) string {
	runes := []rune(input)
	length := len(runes)

	for i, j := 0, length-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}

	return string(runes)
}

func splitReverseConcat(input string) string {
	length := len(input)
	halfLength := length / 2

	firstHalf := input[:halfLength]
	secondHalf := input[halfLength:]

	reversedFirstHalf := reverseString(firstHalf)
	reversedSecondHalf := reverseString(secondHalf)

	return reversedSecondHalf + reversedFirstHalf
}

func hashStringToNumber(s string) uint64 {
	h := fnv.New64a()

	_, err := h.Write([]byte(s))
	if err != nil {
		panic(err)
	}

	return h.Sum64()
}

// runWithTestMySQL boots a MySQL container for the calling test and hands its
// closure a root connection to it. Credentials are derived from the test name so
// the users and databases a test creates cannot collide with the ones it
// connects as. mysqltest owns the container itself.
func runWithTestMySQL(t *testing.T, fn func(ctx context.Context, adminDB *sql.DB)) {
	t.Helper()

	runWithTestMySQLInstance(t, func(ctx context.Context, my *mysqltest.Instance) {
		// Connect as root for admin operations (CREATE USER, GRANT, etc.).
		adminDB := my.Open(t, my.RootConnectionString(t, "allowCleartextPasswords=true", "multiStatements=true"))

		fn(ctx, adminDB)
	})
}

// runWithTestMySQLInstance is runWithTestMySQL without the root connection, for
// the tests that need the container itself — to open a pool of their own
// instrumentation, or to connect as a user they just created.
func runWithTestMySQLInstance(t *testing.T, fn func(ctx context.Context, my *mysqltest.Instance)) {
	t.Helper()

	dbUsername := fmt.Sprintf("u%d", hashStringToNumber(t.Name()))
	dbPassword := reverseString(dbUsername)
	dbName := splitReverseConcat(dbUsername)

	mysqltest.Run(t, fn,
		mysqltest.WithImage(defaultMySQLImage),
		mysqltest.WithCredentials(dbName, dbUsername, dbPassword),
	)
}

func TestQuoteLiteral(T *testing.T) {
	T.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple string",
			input:    "password",
			expected: `'password'`,
		},
		{
			name:     "string with single quotes",
			input:    "user's password",
			expected: `'user''s password'`,
		},
		{
			name:     "string with multiple single quotes",
			input:    "user''s password",
			expected: `'user''''s password'`,
		},
		{
			name:     "empty string",
			input:    "",
			expected: `''`,
		},
		{
			name:     "string with special characters",
			input:    "p@ssw0rd!@#$%",
			expected: `'p@ssw0rd!@#$%'`,
		},
		{
			name:     "trailing backslash is doubled",
			input:    `secret\`,
			expected: `'secret\\'`,
		},
		{
			// MySQL treats backslash as an escape by default, so a naive
			// single-quote-only quoter would let `\'` escape the closing quote and
			// break out of the literal. Doubling the backslash neutralizes it: the
			// value stays entirely inside the quoted string.
			name:     "backslash-quote injection attempt does not break out",
			input:    `x\' OR 1=1 --`,
			expected: `'x\\'' OR 1=1 --'`,
		},
	}

	for _, tt := range tests {
		T.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := quoteLiteral(tt.input)
			test.EqOp(t, tt.expected, result)
		})
	}
}

func TestIsValidPrivilege(T *testing.T) {
	T.Parallel()

	T.Run("valid privileges", func(t *testing.T) {
		t.Parallel()

		validPrivileges := []Privilege{
			PrivilegeSelect,
			PrivilegeInsert,
			PrivilegeUpdate,
			PrivilegeDelete,
			PrivilegeReferences,
			PrivilegeTrigger,
			PrivilegeConnect,
		}

		for _, p := range validPrivileges {
			test.True(t, isValidPrivilege(p), test.Sprintf("expected %q to be valid", p))
		}
	})

	T.Run("invalid privilege", func(t *testing.T) {
		t.Parallel()
		test.False(t, isValidPrivilege("INVALID"))
	})
}

func TestManager_GrantUserAccessToTable_InvalidPrivilege(T *testing.T) {
	T.Parallel()

	T.Run("returns error for invalid privilege", func(t *testing.T) {
		t.Parallel()

		m := NewManager(nil)
		err := m.GrantUserAccessToTable(t.Context(), "user", "schema", "table", "INVALID")
		test.Error(t, err)
		test.StrContains(t, err.Error(), "invalid privilege")
	})
}

func TestNewManager(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		m := NewManager(nil)
		test.NotNil(t, m)
	})
}

func TestManager_CreateUser(T *testing.T) {
	T.Parallel()

	T.Run("success", func(t *testing.T) {
		t.Parallel()

		runWithTestMySQL(t, func(ctx context.Context, adminDB *sql.DB) {
			mgr := NewManager(adminDB)

			username := "testuser"
			password := "testpass123"

			err := mgr.CreateUser(ctx, username, password)
			test.NoError(t, err)

			exists, err := mgr.UserExists(ctx, username)
			test.NoError(t, err)
			test.True(t, exists)
		})
	})

	T.Run("duplicate user", func(t *testing.T) {
		t.Parallel()

		runWithTestMySQL(t, func(ctx context.Context, adminDB *sql.DB) {
			mgr := NewManager(adminDB)

			username := "duplicateuser"
			password := "testpass123"

			err := mgr.CreateUser(ctx, username, password)
			test.NoError(t, err)

			err = mgr.CreateUser(ctx, username, password)
			must.Error(t, err)

			// The sentinel, not just any error: errors/http and errors/grpc both
			// map it to a conflict, and the Postgres twin has reported it since
			// the sentinel gained a producer. A caller should not have to know
			// which driver is underneath to tell a name collision from a fault.
			test.ErrorIs(t, err, database.ErrUserAlreadyExists)

			// And the driver's error underneath it, because 1396 is what sent
			// duplicateUserError looking and a caller should not have to re-run
			// the statement to see it.
			var myErr *mysqldriver.MySQLError
			must.True(t, errors.As(err, &myErr))
			test.EqOp(t, uint16(cannotUser), myErr.Number)
		})
	})

	// 1396 is not duplication-specific — MySQL raises it for any "Operation
	// CREATE USER failed" it declines to elaborate on. The read is what turns it
	// into a finding, so a 1396 naming a user who does not exist keeps the error
	// it arrived with; mapping on the number alone would answer this with a
	// collision that never happened.
	//
	// The error is synthesized because a server cannot be talked into a
	// non-collision 1396 on demand. The read that adjudicates it is real.
	T.Run("1396 naming a user who does not exist is not a collision", func(t *testing.T) {
		t.Parallel()

		runWithTestMySQL(t, func(ctx context.Context, adminDB *sql.DB) {
			mgr := NewManager(adminDB)

			cause := &mysqldriver.MySQLError{
				Number:  cannotUser,
				Message: "Operation CREATE USER failed for 'ghostuser'@'%'",
			}

			got := mgr.duplicateUserError(ctx, "ghostuser", cause)

			test.False(t, errors.Is(got, database.ErrUserAlreadyExists))
			test.ErrorIs(t, got, error(cause))
		})
	})
}

func TestManager_DeleteUser(T *testing.T) {
	T.Parallel()

	T.Run("success", func(t *testing.T) {
		t.Parallel()

		runWithTestMySQL(t, func(ctx context.Context, adminDB *sql.DB) {
			mgr := NewManager(adminDB)

			username := "tobedeleted"
			password := "testpass123"

			err := mgr.CreateUser(ctx, username, password)
			test.NoError(t, err)

			err = mgr.DeleteUser(ctx, username)
			test.NoError(t, err)

			exists, err := mgr.UserExists(ctx, username)
			test.NoError(t, err)
			test.False(t, exists)
		})
	})

	T.Run("delete non-existent user", func(t *testing.T) {
		t.Parallel()

		runWithTestMySQL(t, func(ctx context.Context, adminDB *sql.DB) {
			mgr := NewManager(adminDB)

			err := mgr.DeleteUser(ctx, "nonexistentuser")
			test.NoError(t, err)
		})
	})
}

func TestManager_CreateDatabase(T *testing.T) {
	T.Parallel()

	T.Run("success", func(t *testing.T) {
		t.Parallel()

		runWithTestMySQL(t, func(ctx context.Context, adminDB *sql.DB) {
			mgr := NewManager(adminDB)

			owner := "dbowner"
			err := mgr.CreateUser(ctx, owner, "pass")
			must.NoError(t, err)

			dbName := "testdb"
			err = mgr.CreateDatabase(ctx, dbName, owner)
			test.NoError(t, err)

			exists, err := mgr.DatabaseExists(ctx, dbName)
			test.NoError(t, err)
			test.True(t, exists)
		})
	})
}

func TestManager_DeleteDatabase(T *testing.T) {
	T.Parallel()

	T.Run("success", func(t *testing.T) {
		t.Parallel()

		runWithTestMySQL(t, func(ctx context.Context, adminDB *sql.DB) {
			mgr := NewManager(adminDB)

			owner := "deldbowner"
			err := mgr.CreateUser(ctx, owner, "pass")
			must.NoError(t, err)

			dbName := "deldb"
			err = mgr.CreateDatabase(ctx, dbName, owner)
			must.NoError(t, err)

			err = mgr.DeleteDatabase(ctx, dbName)
			test.NoError(t, err)

			exists, err := mgr.DatabaseExists(ctx, dbName)
			test.NoError(t, err)
			test.False(t, exists)
		})
	})
}

func TestManager_UserExists(T *testing.T) {
	T.Parallel()

	T.Run("nonexistent user", func(t *testing.T) {
		t.Parallel()

		runWithTestMySQL(t, func(ctx context.Context, adminDB *sql.DB) {
			mgr := NewManager(adminDB)

			exists, err := mgr.UserExists(ctx, "nonexistent_user_xyz")
			test.NoError(t, err)
			test.False(t, exists)
		})
	})
}

func TestManager_DatabaseExists(T *testing.T) {
	T.Parallel()

	T.Run("nonexistent database", func(t *testing.T) {
		t.Parallel()

		runWithTestMySQL(t, func(ctx context.Context, adminDB *sql.DB) {
			mgr := NewManager(adminDB)

			exists, err := mgr.DatabaseExists(ctx, "nonexistent_db_xyz")
			test.NoError(t, err)
			test.False(t, exists)
		})
	})
}

func TestManager_UserCanAccessDatabase(T *testing.T) {
	T.Parallel()

	T.Run("reports access a grant actually conferred", func(t *testing.T) {
		t.Parallel()

		runWithTestMySQL(t, func(ctx context.Context, adminDB *sql.DB) {
			mgr := NewManager(adminDB)

			username := "accessuser"
			databaseName := "accessdb"

			must.NoError(t, mgr.CreateUser(ctx, username, "testpass123"))
			must.NoError(t, mgr.CreateDatabase(ctx, databaseName, username))

			canAccess, err := mgr.UserCanAccessDatabase(ctx, username, databaseName)
			test.NoError(t, err)
			test.True(t, canAccess)
		})
	})

	T.Run("reports no access to a database nobody granted", func(t *testing.T) {
		t.Parallel()

		runWithTestMySQL(t, func(ctx context.Context, adminDB *sql.DB) {
			mgr := NewManager(adminDB)

			username := "noaccessuser"
			ownerName := "owneruser"
			databaseName := "noaccessdb"

			must.NoError(t, mgr.CreateUser(ctx, username, "testpass123"))
			must.NoError(t, mgr.CreateUser(ctx, ownerName, "testpass123"))
			must.NoError(t, mgr.CreateDatabase(ctx, databaseName, ownerName))

			canAccess, err := mgr.UserCanAccessDatabase(ctx, username, databaseName)
			test.NoError(t, err)
			test.False(t, canAccess)
		})
	})

	T.Run("reports no access for a user nobody created", func(t *testing.T) {
		t.Parallel()

		runWithTestMySQL(t, func(ctx context.Context, adminDB *sql.DB) {
			mgr := NewManager(adminDB)

			// A grantee with no row is the same answer as a grantee with no
			// grant, and neither is an error: the question was "can this user
			// reach that database", and the answer is no either way.
			canAccess, err := mgr.UserCanAccessDatabase(ctx, "nobody", "somedb")
			test.NoError(t, err)
			test.False(t, canAccess)
		})
	})

	T.Run("does not confuse one user's grant for another's", func(t *testing.T) {
		t.Parallel()

		runWithTestMySQL(t, func(ctx context.Context, adminDB *sql.DB) {
			mgr := NewManager(adminDB)

			// The grantee is matched whole, quotes and host included. A prefix
			// match here would report every user whose name starts with another's
			// as having their access.
			must.NoError(t, mgr.CreateUser(ctx, "granted", "testpass123"))
			must.NoError(t, mgr.CreateUser(ctx, "grantedextra", "testpass123"))
			must.NoError(t, mgr.CreateDatabase(ctx, "sharedname", "granted"))

			canAccess, err := mgr.UserCanAccessDatabase(ctx, "grantedextra", "sharedname")
			test.NoError(t, err)
			test.False(t, canAccess)
		})
	})
}

func TestManager_GrantUserAccessToTable(T *testing.T) {
	T.Parallel()

	T.Run("grants a privilege on one table", func(t *testing.T) {
		t.Parallel()

		runWithTestMySQL(t, func(ctx context.Context, adminDB *sql.DB) {
			mgr := NewManager(adminDB)

			username := "granttableuser"
			schema := "granttabledb"
			table := "things"

			must.NoError(t, mgr.CreateUser(ctx, username, "testpass123"))
			must.NoError(t, mgr.CreateDatabase(ctx, schema, username))

			_, err := adminDB.ExecContext(ctx,
				fmt.Sprintf("CREATE TABLE `%s`.`%s` (id INT PRIMARY KEY)", schema, table))
			must.NoError(t, err)

			test.NoError(t, mgr.GrantUserAccessToTable(ctx, username, schema, table, string(PrivilegeSelect)))
		})
	})

	T.Run("grants each privilege this package admits", func(t *testing.T) {
		t.Parallel()

		runWithTestMySQL(t, func(ctx context.Context, adminDB *sql.DB) {
			mgr := NewManager(adminDB)

			username := "grantalluser"
			schema := "grantalldb"
			table := "things"

			must.NoError(t, mgr.CreateUser(ctx, username, "testpass123"))
			must.NoError(t, mgr.CreateDatabase(ctx, schema, username))

			_, err := adminDB.ExecContext(ctx,
				fmt.Sprintf("CREATE TABLE `%s`.`%s` (id INT PRIMARY KEY)", schema, table))
			must.NoError(t, err)

			// CONNECT is Postgres' word and is on the shared list; MySQL has no
			// such table privilege, so it is deliberately not asked for here.
			for _, privilege := range []Privilege{
				PrivilegeSelect,
				PrivilegeInsert,
				PrivilegeUpdate,
				PrivilegeDelete,
				PrivilegeReferences,
				PrivilegeTrigger,
			} {
				test.NoError(t, mgr.GrantUserAccessToTable(ctx, username, schema, table, string(privilege)),
					test.Sprintf("privilege %q", privilege))
			}
		})
	})

	T.Run("refuses a privilege that is not one, before reaching the server", func(t *testing.T) {
		t.Parallel()

		runWithTestMySQL(t, func(ctx context.Context, adminDB *sql.DB) {
			mgr := NewManager(adminDB)

			// The privilege is interpolated into the statement rather than
			// bound, because GRANT takes no parameters — so the allowlist is
			// what stands between a caller's string and the SQL.
			must.Error(t, mgr.GrantUserAccessToTable(ctx, "user", "schema", "table", "SELECT; DROP DATABASE mysql"))
		})
	})

	T.Run("refuses a lower-cased privilege", func(t *testing.T) {
		t.Parallel()

		runWithTestMySQL(t, func(ctx context.Context, adminDB *sql.DB) {
			mgr := NewManager(adminDB)

			// The allowlist is compared exactly. Accepting "select" here would
			// mean the set of accepted strings is larger than the set written
			// down, which is the property the allowlist exists to have.
			must.Error(t, mgr.GrantUserAccessToTable(ctx, "user", "schema", "table", "select"))
		})
	})

	T.Run("reports a grant on a table nobody has", func(t *testing.T) {
		t.Parallel()

		runWithTestMySQL(t, func(ctx context.Context, adminDB *sql.DB) {
			mgr := NewManager(adminDB)

			must.NoError(t, mgr.CreateUser(ctx, "granttarget", "testpass123"))
			must.NoError(t, mgr.CreateDatabase(ctx, "grantmissingdb", "granttarget"))

			must.Error(t, mgr.GrantUserAccessToTable(ctx, "granttarget", "grantmissingdb", "nosuchtable", "SELECT"))
		})
	})
}
