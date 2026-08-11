package tokens

import (
	"context"
	"time"
)

var _ Issuer = (*NoopIssuer)(nil)

// NoopIssuer is the Issuer that mints and parses nothing. It is exported, and
// returned by NewNoopTokenIssuer, so a caller can depend on the issuer it built
// rather than on the Issuer seam.
type NoopIssuer struct{}

// IssueToken implements the interface.
func (n *NoopIssuer) IssueToken(context.Context, string, time.Duration, map[string]any) (tokenStr, jti string, err error) {
	return "", "", nil
}

// ParseToken implements the interface.
func (n *NoopIssuer) ParseToken(context.Context, string) (Claims, error) {
	return noopClaims{}, nil
}

// NewNoopTokenIssuer returns an Issuer that mints and parses nothing.
func NewNoopTokenIssuer() *NoopIssuer {
	return &NoopIssuer{}
}

// noopClaims is an empty Claims implementation.
type noopClaims struct{}

func (noopClaims) Subject() string                 { return "" }
func (noopClaims) JTI() string                     { return "" }
func (noopClaims) ExpiresAt() time.Time            { return time.Time{} }
func (noopClaims) Get(string) (any, bool)          { return nil, false }
func (noopClaims) GetString(string) (string, bool) { return "", false }
