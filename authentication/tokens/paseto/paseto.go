package paseto

import (
	"context"
	"fmt"
	"time"

	"github.com/primandproper/platform/authentication/tokens"
	"github.com/primandproper/platform/identifiers"
	"github.com/primandproper/platform/observability/logging"
	"github.com/primandproper/platform/observability/tracing"

	"github.com/o1egl/paseto/v2"
)

type (
	signer struct {
		tracer     tracing.Tracer
		logger     logging.Logger
		issuer     string
		audience   string
		signingKey []byte
	}
)

func NewPASETOSigner(logger logging.Logger, tracerProvider tracing.TracerProvider, issuer, audience string, signingKey []byte) (tokens.Issuer, error) {
	s := &signer{
		issuer:     issuer,
		audience:   audience,
		signingKey: signingKey,
		logger:     logging.EnsureLogger(logger),
		tracer:     tracing.NewNamedTracer(tracerProvider, "paseto_signer"),
	}

	return s, nil
}

type tokenPayload struct {
	Expiration time.Time
	IssuedAt   time.Time
	NotBefore  time.Time
	Audience   string
	Issuer     string
	JTI        string
	Subject    string
	AccountID  string `json:"account_id,omitempty"`
	SessionID  string `json:"sid,omitempty"`
}

// IssueToken issues a new PASETO token, optionally including account ID and session ID.
func (s *signer) IssueToken(ctx context.Context, user tokens.User, expiry time.Duration, accountID, sessionID string) (tokenStr, jti string, err error) {
	_, span := s.tracer.StartSpan(ctx)
	defer span.End()

	if expiry <= 0 {
		expiry = time.Minute * 10
	}

	jti = identifiers.New()

	t := tokenPayload{
		Audience:   s.audience,
		Issuer:     s.issuer,
		JTI:        jti,
		Subject:    user.GetID(),
		IssuedAt:   time.Now().UTC(),
		Expiration: time.Now().Add(expiry).UTC(),
		NotBefore:  time.Now().Add(-1 * time.Minute).UTC(),
	}
	if accountID != "" {
		t.AccountID = accountID
	}
	if sessionID != "" {
		t.SessionID = sessionID
	}

	tokenStr, err = paseto.NewV2().Encrypt(s.signingKey, t, "footer")
	if err != nil {
		return "", "", fmt.Errorf("signing token with key length %d: %w", len(s.signingKey), err)
	}

	return tokenStr, jti, nil
}

// ParseUserIDFromToken parses a AccessToken and returns the associated user ID.
func (s *signer) ParseUserIDFromToken(ctx context.Context, providedToken string) (string, error) {
	userID, _, err := s.ParseUserIDAndAccountIDFromToken(ctx, providedToken)
	return userID, err
}

// ParseUserIDAndAccountIDFromToken parses a PASETO token and returns the user ID and optional account ID.
func (s *signer) ParseUserIDAndAccountIDFromToken(ctx context.Context, providedToken string) (userID, accountID string, err error) {
	_, span := s.tracer.StartSpan(ctx)
	defer span.End()

	parsedToken, err := s.decryptToken(providedToken)
	if err != nil {
		return "", "", err
	}

	return parsedToken.Subject, parsedToken.AccountID, nil
}

// ParseSessionIDFromToken extracts the session ID from a PASETO token.
func (s *signer) ParseSessionIDFromToken(ctx context.Context, providedToken string) (string, error) {
	_, span := s.tracer.StartSpan(ctx)
	defer span.End()

	parsedToken, err := s.decryptToken(providedToken)
	if err != nil {
		return "", err
	}

	return parsedToken.SessionID, nil
}

// ParseJTIFromToken extracts the JTI from a PASETO token.
func (s *signer) ParseJTIFromToken(ctx context.Context, providedToken string) (string, error) {
	_, span := s.tracer.StartSpan(ctx)
	defer span.End()

	parsedToken, err := s.decryptToken(providedToken)
	if err != nil {
		return "", err
	}

	return parsedToken.JTI, nil
}

func (s *signer) decryptToken(providedToken string) (*tokenPayload, error) {
	var (
		parsedToken tokenPayload
		footer      string
	)
	if err := paseto.NewV2().Decrypt(providedToken, s.signingKey, &parsedToken, &footer); err != nil {
		s.logger.Error("parsing PASETO token", err)
		return nil, err
	}

	return &parsedToken, nil
}
