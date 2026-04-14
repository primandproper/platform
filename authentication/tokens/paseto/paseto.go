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

// Application-specific claim keys the Parse* helpers look up. Callers that want
// these populated must supply them via extraClaims when calling IssueToken.
const (
	subjectKey   = "sub"
	jtiKey       = "jti"
	accountIDKey = "account_id"
	sessionIDKey = "sid"
)

// IssueToken issues a new PASETO token. The issuer owns the standard claims
// (exp, nbf, iat, aud, iss, sub, jti); callers supply any application-specific
// claims (account_id, sid, etc.) via extraClaims. Passing a reserved-claim key
// in extraClaims returns ErrReservedClaim.
func (s *signer) IssueToken(ctx context.Context, subject string, expiry time.Duration, extraClaims map[string]any) (tokenStr, jti string, err error) {
	_, span := s.tracer.StartSpan(ctx)
	defer span.End()

	if expiry <= 0 {
		expiry = time.Minute * 10
	}

	jti = identifiers.New()

	payload := map[string]any{
		"aud":      s.audience,
		"iss":      s.issuer,
		jtiKey:     jti,
		subjectKey: subject,
		"iat":      time.Now().UTC(),
		"exp":      time.Now().Add(expiry).UTC(),
		"nbf":      time.Now().Add(-1 * time.Minute).UTC(),
	}
	for k, v := range extraClaims {
		if _, reserved := tokens.ReservedClaimKeys[k]; reserved {
			return "", "", fmt.Errorf("%w: %q", tokens.ErrReservedClaim, k)
		}
		payload[k] = v
	}

	tokenStr, err = paseto.NewV2().Encrypt(s.signingKey, payload, "footer")
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

	return claimString(parsedToken, subjectKey), claimString(parsedToken, accountIDKey), nil
}

// ParseSessionIDFromToken extracts the session ID from a PASETO token.
func (s *signer) ParseSessionIDFromToken(ctx context.Context, providedToken string) (string, error) {
	_, span := s.tracer.StartSpan(ctx)
	defer span.End()

	parsedToken, err := s.decryptToken(providedToken)
	if err != nil {
		return "", err
	}

	return claimString(parsedToken, sessionIDKey), nil
}

// ParseJTIFromToken extracts the JTI from a PASETO token.
func (s *signer) ParseJTIFromToken(ctx context.Context, providedToken string) (string, error) {
	_, span := s.tracer.StartSpan(ctx)
	defer span.End()

	parsedToken, err := s.decryptToken(providedToken)
	if err != nil {
		return "", err
	}

	return claimString(parsedToken, jtiKey), nil
}

func (s *signer) decryptToken(providedToken string) (map[string]any, error) {
	var (
		parsedToken map[string]any
		footer      string
	)
	if err := paseto.NewV2().Decrypt(providedToken, s.signingKey, &parsedToken, &footer); err != nil {
		s.logger.Error("parsing PASETO token", err)
		return nil, err
	}

	return parsedToken, nil
}

func claimString(claims map[string]any, key string) string {
	v, ok := claims[key].(string)
	if !ok {
		return ""
	}
	return v
}
