package sessions

import (
	"encoding/base64"
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestNewID(T *testing.T) {
	T.Parallel()

	T.Run("mints a base64url identifier of the documented width", func(t *testing.T) {
		t.Parallel()

		id, err := NewID(t.Context())
		must.NoError(t, err)

		raw, err := base64.RawURLEncoding.DecodeString(id)
		must.NoError(t, err)
		test.EqOp(t, DefaultIDByteLength, len(raw))
	})

	// A cookie carries this value verbatim, so anything needing percent-encoding
	// would either be mangled or would have to be escaped at every boundary.
	T.Run("is safe to put in a cookie unescaped", func(t *testing.T) {
		t.Parallel()

		id, err := NewID(t.Context())
		must.NoError(t, err)

		for i := range len(id) {
			c := id[i]
			ok := (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_'
			test.True(t, ok, test.Sprintf("identifier %q contains %q", id, string(c)))
		}
	})

	// The property identifiers.New cannot offer, and the reason this package
	// does not use it: an xid is sortable, so holding one narrows the search for
	// the next.
	T.Run("successive identifiers do not repeat", func(t *testing.T) {
		t.Parallel()

		seen := map[string]struct{}{}
		for range 250 {
			id, err := NewID(t.Context())
			must.NoError(t, err)

			_, duplicate := seen[id]
			must.False(t, duplicate, must.Sprintf("minted %q twice", id))

			seen[id] = struct{}{}
		}
	})
}
