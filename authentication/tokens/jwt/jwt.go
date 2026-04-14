package jwt

import (
	"context"
	"fmt"
	"time"

	"github.com/primandproper/platform/authentication/tokens"
	"github.com/primandproper/platform/identifiers"
	"github.com/primandproper/platform/observability/logging"
	"github.com/primandproper/platform/observability/tracing"

	"github.com/golang-jwt/jwt/v5"
)

const (
	accountIDKey = "account_id"
	sessionIDKey = "sid"
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

func NewJWTSigner(logger logging.Logger, tracerProvider tracing.TracerProvider, issuer, audience string, signingKey []byte) (tokens.Issuer, error) {
	s := &signer{
		issuer:     issuer,
		audience:   audience,
		signingKey: signingKey,
		logger:     logging.EnsureLogger(logger),
		tracer:     tracing.NewNamedTracer(tracerProvider, "jwt_signer"),
	}

	return s, nil
}

// IssueToken issues a new JSON web token, optionally including account ID and session ID claims.
func (s *signer) IssueToken(ctx context.Context, user tokens.User, expiry time.Duration, accountID, sessionID string) (tokenStr, jti string, err error) {
	_, span := s.tracer.StartSpan(ctx)
	defer span.End()

	if expiry <= 0 {
		expiry = time.Minute * 10
	}

	jti = identifiers.New()

	claims := jwt.MapClaims{
		"exp": jwt.NewNumericDate(time.Now().Add(expiry).UTC()),           /* expiration time */
		"nbf": jwt.NewNumericDate(time.Now().Add(-1 * time.Minute).UTC()), /* not before */
		"iat": jwt.NewNumericDate(time.Now().UTC()),                       /* issued at */
		"aud": s.audience,                                                 /* audience, i.e. server address */
		"iss": s.issuer,                                                   /* issuer */
		"sub": user.GetID(),                                               /* subject */
		"jti": jti,                                                        /* JWT ID */
	}
	if accountID != "" {
		claims[accountIDKey] = accountID
	}
	if sessionID != "" {
		claims[sessionIDKey] = sessionID
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	tokenStr, err = token.SignedString(s.signingKey)
	if err != nil {
		return "", "", err
	}

	return tokenStr, jti, nil
}

// ParseUserIDFromToken parses a AccessToken and returns the associated user ID.
func (s *signer) ParseUserIDFromToken(ctx context.Context, token string) (string, error) {
	userID, _, err := s.ParseUserIDAndAccountIDFromToken(ctx, token)
	return userID, err
}

// ParseUserIDAndAccountIDFromToken parses a JWT and returns the user ID and optional account ID.
func (s *signer) ParseUserIDAndAccountIDFromToken(ctx context.Context, tokenString string) (userID, accountID string, err error) {
	_, span := s.tracer.StartSpan(ctx)
	defer span.End()

	parsedToken, err := s.parseToken(tokenString)
	if err != nil {
		return "", "", err
	}

	subject, err := parsedToken.Claims.GetSubject()
	if err != nil {
		return "", "", err
	}

	// Extract optional account_id claim
	if claims, ok := parsedToken.Claims.(jwt.MapClaims); ok {
		if aid, ok2 := claims[accountIDKey].(string); ok2 && aid != "" {
			return subject, aid, nil
		}
	}

	return subject, "", nil
}

// ParseSessionIDFromToken extracts the session ID claim from a JWT.
func (s *signer) ParseSessionIDFromToken(ctx context.Context, tokenString string) (string, error) {
	_, span := s.tracer.StartSpan(ctx)
	defer span.End()

	parsedToken, err := s.parseToken(tokenString)
	if err != nil {
		return "", err
	}

	if claims, ok := parsedToken.Claims.(jwt.MapClaims); ok {
		if sid, ok2 := claims[sessionIDKey].(string); ok2 {
			return sid, nil
		}
	}

	return "", nil
}

// ParseJTIFromToken extracts the JTI claim from a JWT.
func (s *signer) ParseJTIFromToken(ctx context.Context, tokenString string) (string, error) {
	_, span := s.tracer.StartSpan(ctx)
	defer span.End()

	parsedToken, err := s.parseToken(tokenString)
	if err != nil {
		return "", err
	}

	if claims, ok := parsedToken.Claims.(jwt.MapClaims); ok {
		if jti, ok2 := claims["jti"].(string); ok2 {
			return jti, nil
		}
	}

	return "", nil
}

func (s *signer) parseToken(tokenString string) (*jwt.Token, error) {
	parsedToken, err := jwt.Parse(tokenString, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return s.signingKey, nil
	})
	if err != nil {
		s.logger.Error("parsing JWT", err)
		return nil, err
	}

	return parsedToken, nil
}
