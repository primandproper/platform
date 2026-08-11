package tableaccess

import (
	"context"
	"database/sql"
	stderrors "errors"
	"fmt"
	"strings"

	"github.com/primandproper/platform-go/v10/database"
	"github.com/primandproper/platform-go/v10/errors"

	mysqldriver "github.com/go-sql-driver/mysql"
)

type Privilege string

const (
	PrivilegeSelect     Privilege = "SELECT"
	PrivilegeInsert     Privilege = "INSERT"
	PrivilegeUpdate     Privilege = "UPDATE"
	PrivilegeDelete     Privilege = "DELETE"
	PrivilegeReferences Privilege = "REFERENCES"
	PrivilegeTrigger    Privilege = "TRIGGER"
	PrivilegeConnect    Privilege = "CONNECT"
)

func isValidPrivilege(p Privilege) bool {
	switch p {
	case PrivilegeSelect,
		PrivilegeInsert,
		PrivilegeUpdate,
		PrivilegeDelete,
		PrivilegeReferences,
		PrivilegeTrigger,
		PrivilegeConnect:
		return true
	default:
		return false
	}
}

var _ database.Manager = (*Manager)(nil)

// Manager is the MySQL database.Manager implementation. It is exported,
// and returned by NewManager, so a caller who has chosen MySQL can depend on
// that choice rather than on the interface every dialect's manager shares.
type Manager struct {
	db *sql.DB
}

func NewManager(db *sql.DB) *Manager {
	return &Manager{db: db}
}

// quoteIdent safely wraps a MySQL identifier in backticks,
// doubling any embedded backticks per MySQL quoting rules.
func quoteIdent(id string) string {
	return "`" + strings.ReplaceAll(id, "`", "``") + "`"
}

// quoteLiteral safely wraps a MySQL string literal in single-quotes. MySQL (unlike
// standard SQL) treats backslash as an escape character by default, so a value
// ending in a backslash would otherwise escape the closing quote and break out of
// the literal. Double both backslashes and single-quotes to neutralize that. This
// assumes the default (NO_BACKSLASH_ESCAPES off) SQL mode.
func quoteLiteral(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `''`)
	return `'` + s + `'`
}

// The statements CreateUser sends, in order. Every one of them is a constant:
// the name and the password arrive as bind parameters on the first and are
// referred to by variable thereafter, so neither is ever spelled out in
// statement text. QUOTE is MySQL's own string-literal quoting — what
// quoteLiteral does here, done server-side where the credential already is.
const (
	bindCreateUserArgs   = `SELECT ?, ? INTO @tableaccess_cu_username, @tableaccess_cu_password`
	buildCreateUserSQL   = `SET @tableaccess_cu_sql = CONCAT('CREATE USER ', QUOTE(@tableaccess_cu_username), '@''%'' IDENTIFIED BY ', QUOTE(@tableaccess_cu_password))`
	prepareCreateUser    = `PREPARE tableaccess_cu FROM @tableaccess_cu_sql`
	executeCreateUser    = `EXECUTE tableaccess_cu`
	deallocateCreateUser = `DEALLOCATE PREPARE tableaccess_cu`
	clearCreateUserArgs  = `SET @tableaccess_cu_username = NULL, @tableaccess_cu_password = NULL, @tableaccess_cu_sql = NULL`
)

// cannotUser is the error MySQL raises when a CREATE USER could not be carried
// out. For a plain CREATE USER that is a name already taken.
//
// It is a coarser instrument than the Postgres twin's SQLSTATE: 42710 means
// duplicate object and nothing else, while MySQL spends this one number on every
// "Operation ... failed for ..." it declines to elaborate on, DROP USER included.
// So it is grounds to go and look rather than the finding itself — see
// duplicateUserError.
const cannotUser = 1396

// duplicateUserError decides what a failed CREATE USER should tell the caller.
//
// The error number alone does not establish a duplicate, so it is not reported
// as one until a read says the name is genuinely taken. Answering on the number
// by itself would hand back "user already exists" for any CREATE USER the server
// refused for a reason it lumped under the same code, which is a diagnosis the
// caller cannot check and will act on.
//
// A failing read leaves the original error alone. It is the one thing actually
// known to have happened, and a read that could not run is not evidence of a
// duplicate — swapping it for the sentinel is how a transient failure ends up
// reported as a name collision.
func (m *Manager) duplicateUserError(ctx context.Context, username string, cause error) error {
	var myErr *mysqldriver.MySQLError
	if !stderrors.As(cause, &myErr) || myErr.Number != cannotUser {
		return cause
	}

	exists, err := m.UserExists(ctx, username)
	if err != nil || !exists {
		return cause
	}

	return errors.Join(database.ErrUserAlreadyExists, cause)
}

// CreateUser creates a user with the given password.
//
// The password never appears in statement text. CREATE USER accepts no bind
// parameters, so the direct spelling has to interpolate the credential into the
// SQL — and otelsql copies statement text onto the db.statement span attribute
// whenever LOG_QUERIES is on, which puts a live credential on a span that may
// well be exported to a third party. Binding the arguments into session
// variables and assembling the statement server-side means every statement that
// goes over the wire is a constant.
//
// Session variables outlive a statement, which is why this pins one connection
// for the whole sequence and clears them before handing it back to the pool.
// MySQL has no transactional DDL to lean on the way the Postgres twin does.
//
// A username already in use comes back wrapping database.ErrUserAlreadyExists,
// which errors/http and errors/grpc map to a conflict rather than a 500. The
// driver's own error is preserved underneath it.
func (m *Manager) CreateUser(ctx context.Context, username, password string) (err error) {
	conn, err := m.db.Conn(ctx)
	if err != nil {
		return errors.Wrap(err, "acquiring connection for create user")
	}

	defer func() {
		// However this ended, the connection goes back to the pool holding no
		// credential — a session variable outlives the statement that set it, and
		// the next caller to get this connection can read it.
		if _, clearErr := conn.ExecContext(ctx, clearCreateUserArgs); clearErr != nil {
			err = errors.Join(err, errors.Wrap(clearErr, "clearing create user arguments"))
		}

		if closeErr := conn.Close(); closeErr != nil {
			err = errors.Join(err, errors.Wrap(closeErr, "releasing create user connection"))
		}
	}()

	if _, err = conn.ExecContext(ctx, bindCreateUserArgs, username, password); err != nil {
		return errors.Wrap(err, "binding create user arguments")
	}

	if _, err = conn.ExecContext(ctx, buildCreateUserSQL); err != nil {
		return errors.Wrap(err, "building create user statement")
	}

	if _, err = conn.ExecContext(ctx, prepareCreateUser); err != nil {
		return errors.Wrap(err, "preparing create user statement")
	}

	// Registered only now that there is something to deallocate: MySQL errors on
	// a handler it never issued, which would turn every earlier failure into two.
	defer func() {
		if _, deallocErr := conn.ExecContext(ctx, deallocateCreateUser); deallocErr != nil {
			err = errors.Join(err, errors.Wrap(deallocErr, "deallocating create user statement"))
		}
	}()

	if _, err = conn.ExecContext(ctx, executeCreateUser); err != nil {
		return m.duplicateUserError(ctx, username, errors.Wrap(err, "creating user"))
	}

	return nil
}

func (m *Manager) DeleteUser(ctx context.Context, username string) error {
	_, err := m.db.ExecContext(ctx, fmt.Sprintf("DROP USER IF EXISTS %s@'%%'", quoteLiteral(username)))
	return err
}

func (m *Manager) CreateDatabase(ctx context.Context, dbName, owner string) error {
	if _, err := m.db.ExecContext(ctx, fmt.Sprintf("CREATE DATABASE %s", quoteIdent(dbName))); err != nil {
		return err
	}

	// MySQL has no OWNER concept; grant all privileges instead.
	_, err := m.db.ExecContext(ctx, fmt.Sprintf(
		"GRANT ALL PRIVILEGES ON %s.* TO %s@'%%'",
		quoteIdent(dbName),
		quoteLiteral(owner),
	))
	return err
}

func (m *Manager) DeleteDatabase(ctx context.Context, dbName string) error {
	_, err := m.db.ExecContext(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", quoteIdent(dbName)))
	return err
}

func (m *Manager) UserExists(ctx context.Context, username string) (bool, error) {
	var exists bool
	err := m.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM mysql.user WHERE User = ? AND Host = '%')`, username).Scan(&exists)
	return exists, err
}

func (m *Manager) DatabaseExists(ctx context.Context, dbName string) (bool, error) {
	var exists bool
	err := m.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.SCHEMATA WHERE SCHEMA_NAME = ?)`, dbName).Scan(&exists)
	return exists, err
}

func (m *Manager) UserCanAccessDatabase(ctx context.Context, username, dbName string) (bool, error) {
	var count int
	err := m.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM information_schema.SCHEMA_PRIVILEGES WHERE GRANTEE = CONCAT('''', ?, '''@''%''') AND TABLE_SCHEMA = ?`,
		username, dbName,
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// GrantUserAccessToTable grants a specific privilege on a table to a user.
func (m *Manager) GrantUserAccessToTable(ctx context.Context, username, schema, table, privilege string) error {
	if !isValidPrivilege(Privilege(privilege)) {
		return errors.Newf("invalid privilege: %s", privilege)
	}

	_, err := m.db.ExecContext(ctx, fmt.Sprintf(
		"GRANT %s ON %s.%s TO %s@'%%'",
		privilege,
		quoteIdent(schema),
		quoteIdent(table),
		quoteLiteral(username),
	))
	return err
}
