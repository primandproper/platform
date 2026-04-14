package tokens

import (
	"context"
	"time"
)

type noopTokenIssuer struct{}

// IssueToken implements the interface.
func (n *noopTokenIssuer) IssueToken(context.Context, string, time.Duration, map[string]any) (tokenStr, jti string, err error) {
	return "", "", nil
}

// ParseToken implements the interface.
func (n *noopTokenIssuer) ParseToken(context.Context, string) (Claims, error) {
	return noopClaims{}, nil
}

func NewNoopTokenIssuer() Issuer {
	return &noopTokenIssuer{}
}

// noopClaims is an empty Claims implementation.
type noopClaims struct{}

func (noopClaims) Subject() string                 { return "" }
func (noopClaims) JTI() string                     { return "" }
func (noopClaims) ExpiresAt() time.Time            { return time.Time{} }
func (noopClaims) Get(string) (any, bool)          { return nil, false }
func (noopClaims) GetString(string) (string, bool) { return "", false }
