package passwordreset

import (
	"context"
	"time"

	"github.com/primandproper/platform-go/v14/tenancy"
)

// Store is the persistence seam for password reset tokens.
//
// This package ships a SQL implementation (NewSQLStore) together with the DDL
// it needs (passwordreset/migrations), so adopting the reset flow does not mean
// writing this. The interface exists because the flow and its storage are
// genuinely separable — an application keeping short-lived credentials
// somewhere that is not a SQL database should not have to fork the package to
// keep them.
//
// What an implementation owes its callers is not "these four methods". It is
// the three properties the methods exist to hold, none of which the signatures
// can state:
//
// The secret is never stored. Issue mints it, returns it once, and persists
// something a reader cannot reverse into it.
//
// A token is spendable exactly once, and the store decides which caller spends
// it. Two concurrent Consume calls for one token must produce one success and
// one ErrTokenRedeemed, on separate connections, with no cooperation from the
// caller.
//
// Expiry is refused rather than reclaimed. A token past its deadline is dead to
// Verify and Consume whether or not anything has deleted the row.
//
// Every method takes a tenancy.Scope and none of them offers an unscoped
// variant: an implementation filters on it rather than treating it as a hint. A
// token presented in the wrong scope is ErrTokenNotFound, which is what it is
// from there.
type Store interface {
	// Issue mints a token for a principal, stores its digest, and returns the
	// secret exactly once.
	//
	// It does not check that the user exists — this package reads no user table
	// — and it does not send anything. Both are the caller's, and the second one
	// is where the flow's one real leak lives: an application that returns a
	// different response for a known and an unknown address has built an account
	// enumeration oracle out of a feature intended to protect accounts. Issue
	// for the users you find, say the same thing either way.
	//
	// Issuing again does not invalidate what is outstanding. A user who clicks
	// "email me a link" twice and then opens the first message has a link that
	// works, which is the behavior the alternative quietly breaks.
	Issue(ctx context.Context, scope tenancy.Scope, userID string, ttl time.Duration) (*Issuance, error)

	// Verify resolves a secret to its token without spending it, for the page
	// load that precedes the form.
	//
	// It returns ErrTokenNotFound, ErrTokenExpired, or ErrTokenRedeemed for a
	// token that cannot be spent. A nil error means the token was live when it
	// was read, which is a weaker statement than Consume's: nothing here holds
	// it live until the submit.
	Verify(ctx context.Context, scope tenancy.Scope, secret string) (*Token, error)

	// Consume spends a secret, atomically, and returns the token it spent with
	// its RedeemedAt set.
	//
	// A nil error is the one thing in this package that is a decision rather
	// than an observation: it means this caller, and no other, holds the right
	// to change that user's password. Call it before writing the password, not
	// after — a redemption that succeeds against a password write that fails
	// costs an email, and the reverse leaves a live reset link for an account
	// whose password has just changed.
	Consume(ctx context.Context, scope tenancy.Scope, secret string) (*Token, error)

	// RevokeForUser destroys every unredeemed token a principal holds and
	// reports how many it destroyed.
	//
	// It is what a completed reset calls afterwards, so the links that were
	// outstanding when the password changed stop working. It is also what a
	// user who says "that wasn't me" needs, and what an administrator disabling
	// an account wants to run before walking away from it.
	//
	// Redeemed rows are left alone. They are already unspendable, and they are
	// what makes "this link has already been used" answerable for the life of
	// the link.
	RevokeForUser(ctx context.Context, scope tenancy.Scope, userID string) (int64, error)
}
