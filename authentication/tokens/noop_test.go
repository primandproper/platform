package tokens

import (
	"testing"
	"time"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestNewNoopTokenIssuer(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		issuer := NewNoopTokenIssuer()
		must.NotNil(t, issuer)
	})
}

func TestNoopTokenIssuer(T *testing.T) {
	T.Parallel()

	T.Run("IssueToken returns empty values and no error", func(t *testing.T) {
		t.Parallel()

		issuer := NewNoopTokenIssuer()

		tokenStr, jti, err := issuer.IssueToken(t.Context(), t.Name(), time.Minute, nil)
		test.NoError(t, err)
		test.EqOp(t, "", tokenStr)
		test.EqOp(t, "", jti)
	})

	T.Run("ParseToken returns empty claims and no error", func(t *testing.T) {
		t.Parallel()

		issuer := NewNoopTokenIssuer()

		claims, err := issuer.ParseToken(t.Context(), t.Name())
		must.NoError(t, err)
		must.NotNil(t, claims)

		test.EqOp(t, "", claims.Subject())
		test.EqOp(t, "", claims.JTI())
		test.True(t, claims.ExpiresAt().IsZero())

		v, ok := claims.Get("anything")
		test.False(t, ok)
		test.Nil(t, v)

		s, ok := claims.GetString("anything")
		test.False(t, ok)
		test.EqOp(t, "", s)
	})
}
