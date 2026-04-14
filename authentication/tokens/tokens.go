package tokens

import (
	"context"
	"time"
)

type (
	User interface {
		GetID() string
		GetEmail() string
		GetUsername() string
		GetFirstName() string
		GetLastName() string
		FullName() string
	}

	Issuer interface {
		IssueToken(ctx context.Context, user User, expiry time.Duration, accountID, sessionID string) (tokenStr, jti string, err error)
		ParseUserIDFromToken(ctx context.Context, token string) (string, error)
		ParseUserIDAndAccountIDFromToken(ctx context.Context, token string) (userID, accountID string, err error)
		ParseSessionIDFromToken(ctx context.Context, token string) (string, error)
		ParseJTIFromToken(ctx context.Context, token string) (string, error)
	}
)
