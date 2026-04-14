package tokens

import (
	"context"
	"time"

	platformerrors "github.com/primandproper/platform/errors"
)

// ErrReservedClaim indicates that a caller passed a JWT registered-claim key in extraClaims.
// Reserved claim keys (iss, sub, aud, exp, nbf, iat, jti) are owned by the issuer and cannot
// be overridden by callers.
var ErrReservedClaim = platformerrors.New("reserved claim key in extraClaims")

// ReservedClaimKeys is the set of JWT registered claim names (RFC 7519) the issuer owns.
// Callers MUST NOT include these in extraClaims passed to IssueToken.
var ReservedClaimKeys = map[string]struct{}{
	"iss": {},
	"sub": {},
	"aud": {},
	"exp": {},
	"nbf": {},
	"iat": {},
	"jti": {},
}

// Issuer issues and parses authentication tokens. Implementations own the standard
// claim mechanics (sub, jti, iat, nbf, exp, aud, iss); callers supply any
// application-specific claims (account_id, sid, etc.) via extraClaims and look them
// up after parsing via the application-specific Parse* helpers below.
type Issuer interface {
	IssueToken(ctx context.Context, subject string, expiry time.Duration, extraClaims map[string]any) (tokenStr, jti string, err error)
	ParseUserIDFromToken(ctx context.Context, token string) (string, error)
	ParseUserIDAndAccountIDFromToken(ctx context.Context, token string) (userID, accountID string, err error)
	ParseSessionIDFromToken(ctx context.Context, token string) (string, error)
	ParseJTIFromToken(ctx context.Context, token string) (string, error)
}
