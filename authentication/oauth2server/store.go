package oauth2server

import (
	"context"
	"time"
)

// Store is where an authorization server's four kinds of state live:
// registered clients, authorization codes, access tokens, and refresh tokens.
//
// It is one interface rather than four because three of the four operations
// that matter span two of them. Redeeming a code mints a token pair; detecting
// a replayed refresh token revokes a family that includes access tokens;
// revoking a registration has to reach whatever it issued. Four interfaces
// would leave a caller holding four handles that have to be to the same
// database for any of that to be atomic, and nothing would say so.
//
// # Why the consuming methods are shaped the way they are
//
// ConsumeAuthorizationCode and ConsumeRefreshToken are not "read, then mark".
// A caller cannot write that pair correctly against a shared table: two
// requests carrying the same code both read it unredeemed, both mint a token
// pair, and the credential that was supposed to be single-use was used twice.
// The check and the mark are therefore one method, and an implementation owes
// its callers that they happen atomically — one statement, one transaction,
// one lock, whatever the backend makes available.
//
// Expiry is inside the same operation for the same reason, and it is the case
// a map-backed store gets for free and a table does not. A store that checks
// `expires_at > now` in Go, between the read and the write, has a window in
// which a code expires and is redeemed anyway. The guard belongs in the
// predicate.
//
// # What is stored
//
// Digests, never credentials. Every method here takes and returns a hash — see
// Hash — and no implementation ever sees the value the client holds. That is
// invisible in a map that dies with the process and is the difference between
// a leaked database backup and a leaked database backup that authorizes people.
//
// # Conformance
//
// authentication/oauth2server/oauth2servertest holds the behavior every
// implementation owes, written once. Run it against any Store, including one a
// consumer writes.
type Store interface {
	// CreateClient records a registration. An identifier already in use is
	// ErrClientExists rather than an overwrite: registrations are created by
	// anonymous callers, and a silent overwrite would let one of them take over
	// another's client by guessing an identifier.
	CreateClient(ctx context.Context, client *Client) error

	// GetClient reads a registration. A registration past its ExpiresAt is
	// ErrExpired, not a value the caller has to check.
	GetClient(ctx context.Context, clientID string) (*Client, error)

	// DeleteClient removes a registration. A registration that was already
	// gone is not an error — the caller wanted it gone.
	DeleteClient(ctx context.Context, clientID string) error

	// CreateAuthorizationCode records an issued code.
	CreateAuthorizationCode(ctx context.Context, code *AuthorizationCode) error

	// ConsumeAuthorizationCode marks the code redeemed and returns it, in one
	// atomic operation.
	//
	// A code that was already redeemed returns the record *and*
	// ErrAlreadyRedeemed. The record is not a courtesy: RFC 6749 §4.1.2 says a
	// replayed code should revoke what it previously issued, and the caller
	// cannot find those tokens without knowing which family the code belongs
	// to.
	ConsumeAuthorizationCode(ctx context.Context, hash string) (*AuthorizationCode, error)

	// CreateAccessToken records an issued access token.
	CreateAccessToken(ctx context.Context, token *AccessToken) error

	// GetAccessToken reads an access token. Expired or revoked is ErrExpired —
	// a resource server asking about a token it holds wants a straight answer,
	// and "expired" and "revoked" are the same answer to it.
	GetAccessToken(ctx context.Context, hash string) (*AccessToken, error)

	// RevokeAccessToken marks one access token revoked. A token that is absent
	// or already revoked is not an error: RFC 7009 §2.2 requires the
	// revocation endpoint to answer 200 either way, and a store that
	// distinguished them would be inviting the endpoint to leak which tokens
	// exist.
	RevokeAccessToken(ctx context.Context, hash string) error

	// CreateRefreshToken records an issued refresh token.
	CreateRefreshToken(ctx context.Context, token *RefreshToken) error

	// ConsumeRefreshToken marks the token redeemed and returns it, in one
	// atomic operation. As with ConsumeAuthorizationCode, a replay returns the
	// record alongside ErrAlreadyRedeemed so that the family can be revoked.
	ConsumeRefreshToken(ctx context.Context, hash string) (*RefreshToken, error)

	// GetRefreshToken reads a refresh token without consuming it.
	//
	// It exists for /revoke, which needs the record to learn whose token this
	// is and which family to end — and must not spend the token to find out.
	// An already-redeemed token is still returned: a sign-out arriving after a
	// rotation is the ordinary case, and the family it names is exactly what
	// needs revoking. Expired or revoked is ErrExpired.
	GetRefreshToken(ctx context.Context, hash string) (*RefreshToken, error)

	// RevokeRefreshToken marks one refresh token revoked, without touching the
	// rest of its family. This is what /revoke does; RevokeFamily is what a
	// detected replay does.
	RevokeRefreshToken(ctx context.Context, hash string) error

	// RevokeFamily revokes every access and refresh token in a family and
	// reports how many records it touched.
	//
	// The count is for the caller's metric, not its control flow — a family
	// whose tokens have all expired legitimately revokes nothing.
	RevokeFamily(ctx context.Context, familyID string) (int64, error)

	// Sweep removes records whose deadlines have passed as of now, reporting
	// how many it removed.
	//
	// It is a garbage collector, not a security control: every read above
	// already refuses an expired record, so a row this has not reached yet is
	// unusable. What it stops is the table growing with every code ever
	// issued, which under dynamic registration is a table an anonymous caller
	// can add to.
	Sweep(ctx context.Context, now time.Time) (int64, error)

	// Close releases whatever the implementation holds.
	Close() error
}

// SweepInterval bounds are shared by the store implementations that run their
// own sweep goroutine, so that "how often" means the same thing in both.
const (
	// DefaultSweepInterval is how often a store with a sweeper started removes
	// dead records when nothing says otherwise.
	//
	// Ten minutes, which is roughly two authorization-code lifetimes: often
	// enough that the code table stays near its steady-state size, rarely
	// enough that it is not a recurring full-table delete.
	DefaultSweepInterval = 10 * time.Minute
)
