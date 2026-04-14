package paseto

import (
	"crypto/ed25519"
	"testing"
	"time"

	"github.com/primandproper/platform/authentication/tokens"
	loggingnoop "github.com/primandproper/platform/observability/logging/noop"
	tracingnoop "github.com/primandproper/platform/observability/tracing/noop"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	exampleSigningKey = "HEREISA32CHARSECRETWHICHISMADEUP"
	ed25519SigningKey = "HEREISA64CHARSECRETWHICHISMADEUPHEREISA64CHARSECRETWHICHISMADEUP"
	exampleToken      = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJhdWQiOiJUZXN0X2p3dFNpZ25lcl9Jc3N1ZUpXVC9zdGFuZGFyZCIsImV4cCI6MTcyNzU3MDU0OCwiaWF0IjoxNzI3NTY5OTQ4LCJpc3MiOiJkaW5uZXJkb25lYmV0dGVyIiwianRpIjoiY3JzYTA3NnRnM3FkdG1jY3E5MTAiLCJuYmYiOjE3Mjc1Njk4ODgsInN1YiI6ImNyc2EwNzZ0ZzNxZHRtY2NxOTBnIn0.tMASrQBoYAq4n1iwOElLqUQsYOARX5T1qxo8RKhvaAg"
	exampleExpiry     = time.Minute * 10
	exampleSubject    = "user_id"
)

func Test_signer_IssueToken(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		s, err := NewPASETOSigner(loggingnoop.NewLogger(), tracingnoop.NewTracerProvider(), "platform-test", t.Name(), []byte(exampleSigningKey))
		require.NoError(t, err)

		ctx := t.Context()

		actual, _, err := s.IssueToken(ctx, exampleSubject, exampleExpiry, nil)
		assert.NoError(t, err)

		parsed, err := s.ParseUserIDFromToken(ctx, actual)
		assert.NoError(t, err)
		assert.Equal(t, exampleSubject, parsed)
	})

	T.Run("with account ID and session ID", func(t *testing.T) {
		t.Parallel()

		s, err := NewPASETOSigner(loggingnoop.NewLogger(), tracingnoop.NewTracerProvider(), "platform-test", t.Name(), []byte(exampleSigningKey))
		require.NoError(t, err)

		ctx := t.Context()

		accountID := "account_123"
		sessionID := "session_456"

		tokenStr, jti, err := s.IssueToken(ctx, exampleSubject, exampleExpiry, map[string]any{
			"account_id": accountID,
			"sid":        sessionID,
		})
		assert.NoError(t, err)
		assert.NotEmpty(t, tokenStr)
		assert.NotEmpty(t, jti)

		// Verify user ID and account ID
		parsedUserID, parsedAccountID, err := s.ParseUserIDAndAccountIDFromToken(ctx, tokenStr)
		assert.NoError(t, err)
		assert.Equal(t, exampleSubject, parsedUserID)
		assert.Equal(t, accountID, parsedAccountID)

		// Verify session ID
		parsedSessionID, err := s.ParseSessionIDFromToken(ctx, tokenStr)
		assert.NoError(t, err)
		assert.Equal(t, sessionID, parsedSessionID)

		// Verify JTI
		parsedJTI, err := s.ParseJTIFromToken(ctx, tokenStr)
		assert.NoError(t, err)
		assert.Equal(t, jti, parsedJTI)
	})

	T.Run("rejects reserved claim key", func(t *testing.T) {
		t.Parallel()

		s, err := NewPASETOSigner(loggingnoop.NewLogger(), tracingnoop.NewTracerProvider(), "platform-test", t.Name(), []byte(exampleSigningKey))
		require.NoError(t, err)

		_, _, err = s.IssueToken(t.Context(), exampleSubject, exampleExpiry, map[string]any{
			"sub": "attacker_id",
		})
		assert.ErrorIs(t, err, tokens.ErrReservedClaim)
	})
}

func Test_signer_ParseUserIDFromToken(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		s, err := NewPASETOSigner(loggingnoop.NewLogger(), tracingnoop.NewTracerProvider(), "platform-test", t.Name(), []byte(exampleSigningKey))
		require.NoError(t, err)

		ctx := t.Context()

		issuedToken, _, err := s.IssueToken(ctx, exampleSubject, exampleExpiry, nil)
		assert.NoError(t, err)

		actual, err := s.ParseUserIDFromToken(ctx, issuedToken)
		assert.NoError(t, err)
		assert.Equal(t, exampleSubject, actual)
	})

	T.Run("with invalid algo", func(t *testing.T) {
		t.Parallel()

		token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, jwt.MapClaims{})

		cryptoSigner := ed25519.PrivateKey(ed25519SigningKey)
		tokenString, err := token.SignedString(cryptoSigner)
		require.NoError(t, err)

		ctx := t.Context()

		s, err := NewPASETOSigner(loggingnoop.NewLogger(), tracingnoop.NewTracerProvider(), "platform-test", t.Name(), []byte(exampleSigningKey))
		require.NoError(t, err)

		actual, err := s.ParseUserIDFromToken(ctx, tokenString)
		assert.Error(t, err)
		assert.Empty(t, actual)
	})

	T.Run("with invalid key", func(t *testing.T) {
		t.Parallel()

		s, err := NewPASETOSigner(loggingnoop.NewLogger(), tracingnoop.NewTracerProvider(), "platform-test", t.Name(), []byte(exampleSigningKey))
		require.NoError(t, err)

		s.(*signer).signingKey = nil

		ctx := t.Context()

		actual, err := s.ParseUserIDFromToken(ctx, exampleToken)
		assert.Error(t, err)
		assert.Empty(t, actual)
	})
}

func Test_signer_ParseSessionIDFromToken(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		s, err := NewPASETOSigner(loggingnoop.NewLogger(), tracingnoop.NewTracerProvider(), "platform-test", t.Name(), []byte(exampleSigningKey))
		require.NoError(t, err)

		ctx := t.Context()

		sessionID := "session_abc"

		tokenStr, _, err := s.IssueToken(ctx, exampleSubject, exampleExpiry, map[string]any{"sid": sessionID})
		require.NoError(t, err)

		actual, err := s.ParseSessionIDFromToken(ctx, tokenStr)
		assert.NoError(t, err)
		assert.Equal(t, sessionID, actual)
	})

	T.Run("with missing field", func(t *testing.T) {
		t.Parallel()

		s, err := NewPASETOSigner(loggingnoop.NewLogger(), tracingnoop.NewTracerProvider(), "platform-test", t.Name(), []byte(exampleSigningKey))
		require.NoError(t, err)

		ctx := t.Context()

		tokenStr, _, err := s.IssueToken(ctx, exampleSubject, exampleExpiry, nil)
		require.NoError(t, err)

		actual, err := s.ParseSessionIDFromToken(ctx, tokenStr)
		assert.NoError(t, err)
		assert.Empty(t, actual)
	})

	T.Run("with invalid token", func(t *testing.T) {
		t.Parallel()

		s, err := NewPASETOSigner(loggingnoop.NewLogger(), tracingnoop.NewTracerProvider(), "platform-test", t.Name(), []byte(exampleSigningKey))
		require.NoError(t, err)

		ctx := t.Context()

		actual, err := s.ParseSessionIDFromToken(ctx, "not-a-valid-token")
		assert.Error(t, err)
		assert.Empty(t, actual)
	})
}

func Test_signer_ParseJTIFromToken(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		s, err := NewPASETOSigner(loggingnoop.NewLogger(), tracingnoop.NewTracerProvider(), "platform-test", t.Name(), []byte(exampleSigningKey))
		require.NoError(t, err)

		ctx := t.Context()

		tokenStr, expectedJTI, err := s.IssueToken(ctx, exampleSubject, exampleExpiry, nil)
		require.NoError(t, err)
		require.NotEmpty(t, expectedJTI)

		actual, err := s.ParseJTIFromToken(ctx, tokenStr)
		assert.NoError(t, err)
		assert.Equal(t, expectedJTI, actual)
	})

	T.Run("with invalid token", func(t *testing.T) {
		t.Parallel()

		s, err := NewPASETOSigner(loggingnoop.NewLogger(), tracingnoop.NewTracerProvider(), "platform-test", t.Name(), []byte(exampleSigningKey))
		require.NoError(t, err)

		ctx := t.Context()

		actual, err := s.ParseJTIFromToken(ctx, "not-a-valid-token")
		assert.Error(t, err)
		assert.Empty(t, actual)
	})
}
