package tokens

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewNoopTokenIssuer(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		issuer := NewNoopTokenIssuer()
		require.NotNil(t, issuer)
	})
}

func TestNoopTokenIssuer(T *testing.T) {
	T.Parallel()

	T.Run("IssueToken returns empty values and no error", func(t *testing.T) {
		t.Parallel()

		issuer := NewNoopTokenIssuer()

		tokenStr, jti, err := issuer.IssueToken(t.Context(), t.Name(), time.Minute, nil)
		assert.NoError(t, err)
		assert.Empty(t, tokenStr)
		assert.Empty(t, jti)
	})

	T.Run("ParseUserIDFromToken returns empty string and no error", func(t *testing.T) {
		t.Parallel()

		issuer := NewNoopTokenIssuer()

		userID, err := issuer.ParseUserIDFromToken(t.Context(), t.Name())
		assert.NoError(t, err)
		assert.Empty(t, userID)
	})

	T.Run("ParseUserIDAndAccountIDFromToken returns empty values and no error", func(t *testing.T) {
		t.Parallel()

		issuer := NewNoopTokenIssuer()

		userID, accountID, err := issuer.ParseUserIDAndAccountIDFromToken(t.Context(), t.Name())
		assert.NoError(t, err)
		assert.Empty(t, userID)
		assert.Empty(t, accountID)
	})

	T.Run("ParseSessionIDFromToken returns empty string and no error", func(t *testing.T) {
		t.Parallel()

		issuer := NewNoopTokenIssuer()

		sessionID, err := issuer.ParseSessionIDFromToken(t.Context(), t.Name())
		assert.NoError(t, err)
		assert.Empty(t, sessionID)
	})

	T.Run("ParseJTIFromToken returns empty string and no error", func(t *testing.T) {
		t.Parallel()

		issuer := NewNoopTokenIssuer()

		jti, err := issuer.ParseJTIFromToken(t.Context(), t.Name())
		assert.NoError(t, err)
		assert.Empty(t, jti)
	})
}
