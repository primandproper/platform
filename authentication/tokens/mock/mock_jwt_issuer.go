package mocktokens

import (
	"context"
	"time"

	"github.com/primandproper/platform/authentication/tokens"

	"github.com/stretchr/testify/mock"
)

var _ tokens.Issuer = (*Issuer)(nil)

type Issuer struct {
	mock.Mock
}

func (m *Issuer) IssueToken(ctx context.Context, user tokens.User, expiry time.Duration, accountID, sessionID string) (tokenStr, jti string, err error) {
	returnValues := m.Called(ctx, user, expiry, accountID, sessionID)
	return returnValues.String(0), returnValues.String(1), returnValues.Error(2)
}

func (m *Issuer) ParseUserIDFromToken(ctx context.Context, token string) (string, error) {
	userID, _, err := m.ParseUserIDAndAccountIDFromToken(ctx, token)
	return userID, err
}

func (m *Issuer) ParseUserIDAndAccountIDFromToken(ctx context.Context, token string) (userID, accountID string, err error) {
	returnValues := m.Called(ctx, token)
	return returnValues.String(0), returnValues.String(1), returnValues.Error(2)
}

func (m *Issuer) ParseSessionIDFromToken(ctx context.Context, token string) (string, error) {
	returnValues := m.Called(ctx, token)
	return returnValues.String(0), returnValues.Error(1)
}

func (m *Issuer) ParseJTIFromToken(ctx context.Context, token string) (string, error) {
	returnValues := m.Called(ctx, token)
	return returnValues.String(0), returnValues.Error(1)
}
