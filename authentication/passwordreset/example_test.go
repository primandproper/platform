package passwordreset_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/primandproper/platform-go/v13/authentication/passwordreset"
	"github.com/primandproper/platform-go/v13/authentication/passwordreset/migrations"
	"github.com/primandproper/platform-go/v13/database"
	"github.com/primandproper/platform-go/v13/database/dialect"
	"github.com/primandproper/platform-go/v13/database/sqlite"
	"github.com/primandproper/platform-go/v13/tenancy"
)

// The whole flow, in the order that fails safe: issue a token, mail the secret,
// spend it when the user submits the form, then revoke whatever else was
// outstanding.
//
// The password write goes between Consume and RevokeForUser. Consuming first
// costs the user another email if the write then fails; writing first would
// leave a live reset link for an account whose password had just changed.
func Example() {
	ctx := context.Background()

	client, cleanup := exampleClient(ctx)
	defer cleanup()

	store, err := passwordreset.NewSQLStore(&passwordreset.Config{}, client)
	if err != nil {
		panic(err)
	}

	scope := tenancy.Global()

	// Somebody asked for a reset. Say the same thing whether or not the address
	// belonged to a user — the response that differs is an account enumeration
	// oracle built out of a feature meant to protect accounts.
	issuance, err := store.Issue(ctx, scope, "user_01", time.Hour)
	if err != nil {
		panic(err)
	}

	// The secret exists here and nowhere else. It goes into the link and is
	// forgotten; the store holds a digest it cannot reverse.
	fmt.Println("mailing a link carrying", len(issuance.Secret), "characters")

	// They followed the link. Verify spends nothing, so reloading the page —
	// or thinking better of it and coming back later — leaves the token usable.
	if _, err = store.Verify(ctx, scope, issuance.Secret); err != nil {
		panic(err)
	}

	// They submitted the form. Consume is the decision: exactly one caller
	// leaves this line holding the right to change that password.
	token, err := store.Consume(ctx, scope, issuance.Secret)
	if err != nil {
		panic(err)
	}

	fmt.Println("resetting the password for", token.UserID)

	// ... write the new password hash here, through whatever store owns users.

	// Everything else they had outstanding stops working.
	revoked, err := store.RevokeForUser(ctx, scope, token.UserID)
	if err != nil {
		panic(err)
	}

	fmt.Println("revoked", revoked, "other outstanding tokens")

	// The link cannot be used twice, and the store says so rather than the
	// caller having to remember.
	_, err = store.Consume(ctx, scope, issuance.Secret)
	fmt.Println("second attempt:", errors.Is(err, passwordreset.ErrTokenRedeemed))

	// Output:
	// mailing a link carrying 43 characters
	// resetting the password for user_01
	// revoked 0 other outstanding tokens
	// second attempt: true
}

// exampleClient stands up a throwaway SQLite database with the token table
// created from the DDL this package ships.
func exampleClient(ctx context.Context) (client database.Client, cleanup func()) {
	dir, err := os.MkdirTemp("", "passwordreset-example")
	if err != nil {
		panic(err)
	}

	client, err = sqlite.NewDatabaseClient(ctx,
		&exampleConfig{path: filepath.Join(dir, "reset.db")})
	if err != nil {
		panic(err)
	}

	stmts, err := migrations.Statements(dialect.SQLite, passwordreset.DefaultTablePrefix)
	if err != nil {
		panic(err)
	}

	for _, stmt := range stmts {
		if _, err = client.Writer().ExecContext(ctx, stmt); err != nil {
			panic(err)
		}
	}

	return client, func() {
		_ = client.Close()
		_ = os.RemoveAll(dir)
	}
}

type exampleConfig struct {
	path string
}

var _ database.ClientConfig = (*exampleConfig)(nil)

func (c *exampleConfig) GetReadConnectionString() string   { return c.path }
func (c *exampleConfig) GetWriteConnectionString() string  { return c.path }
func (c *exampleConfig) GetMaxPingAttempts() uint64        { return 1 }
func (c *exampleConfig) GetPingWaitPeriod() time.Duration  { return time.Second }
func (c *exampleConfig) GetMaxIdleConns() int              { return 1 }
func (c *exampleConfig) GetMaxOpenConns() int              { return 1 }
func (c *exampleConfig) GetConnMaxLifetime() time.Duration { return time.Minute }
