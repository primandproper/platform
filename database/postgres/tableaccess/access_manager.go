package tableaccess

import (
	"context"
	"database/sql"
	stderrors "errors"
	"fmt"
	"strings"

	"github.com/primandproper/platform-go/v10/database"
	"github.com/primandproper/platform-go/v10/errors"

	"github.com/jackc/pgx/v5/pgconn"
)

type Privilege string

const (
	PrivilegeSelect     Privilege = "SELECT"
	PrivilegeInsert     Privilege = "INSERT"
	PrivilegeUpdate     Privilege = "UPDATE"
	PrivilegeDelete     Privilege = "DELETE"
	PrivilegeTruncate   Privilege = "TRUNCATE"
	PrivilegeReferences Privilege = "REFERENCES"
	PrivilegeTrigger    Privilege = "TRIGGER"
	PrivilegeConnect    Privilege = "CONNECT" // for database-level ops
)

func isValidPrivilege(p Privilege) bool {
	switch p {
	case PrivilegeSelect,
		PrivilegeInsert,
		PrivilegeUpdate,
		PrivilegeDelete,
		PrivilegeTruncate,
		PrivilegeReferences,
		PrivilegeTrigger,
		PrivilegeConnect:
		return true
	default:
		return false
	}
}

type manager struct {
	db *sql.DB
}

func NewManager(db *sql.DB) database.Manager {
	return &manager{db: db}
}

// quoteIdent safely wraps a Postgres identifier in double‑quotes,
// doubling any embedded double‑quotes per the SQL spec.
func quoteIdent(id string) string {
	return `"` + strings.ReplaceAll(id, `"`, `""`) + `"`
}

// bindCreateUserArgs stashes the new role's name and password in transaction-local
// settings. Both travel as bind parameters, so neither reaches the statement text.
//
// The setting names are two-part on purpose: Postgres accepts a custom setting
// only under an "extension.name" spelling, and rejects anything with more or
// fewer dots.
const bindCreateUserArgs = `SELECT set_config('tableaccess.create_user_username', $1, true),
       set_config('tableaccess.create_user_password', $2, true)`

// createUserFromSettings reads those settings back and quotes them server-side.
// format's %I and %L are Postgres' own identifier and literal quoting — the same
// job quoteIdent does here, done where the credential does not have to be spelled
// out to get there.
const createUserFromSettings = `DO $do$
BEGIN
	EXECUTE format(
		'CREATE USER %I WITH PASSWORD %L',
		current_setting('tableaccess.create_user_username'),
		current_setting('tableaccess.create_user_password')
	);
END
$do$`

// duplicateObject is the SQLSTATE Postgres raises for CREATE USER against a
// role that already exists. There is no CREATE USER IF NOT EXISTS to lean on, so
// the code is the only thing that separates "somebody already made this user"
// from a connection that dropped mid-statement.
//
// A DO block does not swallow it: nothing here catches the exception, so
// PL/pgSQL re-raises it with its SQLSTATE intact and the driver still hands back
// a *pgconn.PgError carrying this code.
const duplicateObject = "42710"

// CreateUser creates a role with the given password.
//
// The password never appears in statement text. CREATE USER is a utility
// statement and accepts no bind parameters, so the direct spelling has to
// interpolate the credential into the SQL — and otelsql copies statement text
// onto the db.statement span attribute whenever LOG_QUERIES is on, which puts a
// live credential on a span that may well be exported to a third party. Binding
// the arguments into settings and letting the server do the quoting means every
// statement that goes over the wire is a constant.
//
// The transaction is what scopes the settings: set_config's local flag ties them
// to it, so they are gone whether it commits or rolls back, and no later caller
// on a pooled connection can read them. CREATE ROLE is transactional in Postgres,
// so the role and the settings share one unit of work.
//
// A username already in use comes back wrapping database.ErrUserAlreadyExists,
// which errors/http and errors/grpc map to a conflict rather than a 500. The
// driver's own error is preserved underneath it: the SQLSTATE is what identified
// the failure, and a caller that wants the detail should not have to re-run the
// statement to get it.
func (p *manager) CreateUser(ctx context.Context, username, password string) (err error) {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return errors.Wrap(err, "beginning create user transaction")
	}

	defer func() {
		// Rolling back an already-committed transaction is ErrTxDone and means
		// only that the happy path happened; anything else is a connection in a
		// state the caller should hear about.
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !stderrors.Is(rollbackErr, sql.ErrTxDone) {
			err = errors.Join(err, errors.Wrap(rollbackErr, "rolling back create user transaction"))
		}
	}()

	if _, err = tx.ExecContext(ctx, bindCreateUserArgs, username, password); err != nil {
		return errors.Wrap(err, "binding create user arguments")
	}

	if _, err = tx.ExecContext(ctx, createUserFromSettings); err != nil {
		var pgErr *pgconn.PgError
		if stderrors.As(err, &pgErr) && pgErr.Code == duplicateObject {
			return errors.Join(database.ErrUserAlreadyExists, errors.Wrap(err, "creating user"))
		}

		return errors.Wrap(err, "creating user")
	}

	return errors.Wrap(tx.Commit(), "committing create user transaction")
}

func (p *manager) DeleteUser(ctx context.Context, username string) error {
	_, err := p.db.ExecContext(ctx, fmt.Sprintf("DROP USER IF EXISTS %s", quoteIdent(username)))
	return err
}

func (p *manager) CreateDatabase(ctx context.Context, dbName, owner string) error {
	_, err := p.db.ExecContext(ctx, fmt.Sprintf(
		"CREATE DATABASE %s OWNER %s",
		quoteIdent(dbName),
		quoteIdent(owner),
	))
	return err
}

func (p *manager) DeleteDatabase(ctx context.Context, dbName string) error {
	_, err := p.db.ExecContext(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", quoteIdent(dbName)))
	return err
}

func (p *manager) UserExists(ctx context.Context, username string) (bool, error) {
	var exists bool
	err := p.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = $1)`, username).Scan(&exists)
	return exists, err
}

func (p *manager) DatabaseExists(ctx context.Context, dbName string) (bool, error) {
	var exists bool
	err := p.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)`, dbName).Scan(&exists)
	return exists, err
}

func (p *manager) UserCanAccessDatabase(ctx context.Context, username, dbName string) (bool, error) {
	var hasPrivilege bool
	err := p.db.QueryRowContext(ctx, `SELECT has_database_privilege($1, $2, 'CONNECT')`, username, dbName).Scan(&hasPrivilege)
	return hasPrivilege, err
}

// GrantUserAccessToTable grants a specific privilege on a table to a user.
func (p *manager) GrantUserAccessToTable(ctx context.Context, username, schema, table, privilege string) error {
	if !isValidPrivilege(Privilege(privilege)) {
		return errors.Newf("invalid privilege: %s", privilege)
	}

	_, err := p.db.ExecContext(ctx, fmt.Sprintf("GRANT %s ON TABLE %s TO %s", privilege, fmt.Sprintf("%s.%s", quoteIdent(schema), quoteIdent(table)), quoteIdent(username)))
	return err
}
