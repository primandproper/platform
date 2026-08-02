package encoding

import (
	"testing"

	"github.com/shoenig/test"
)

func TestNewContentType(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		ct, err := NewContentType(Config{ContentType: "application/json"})
		test.NoError(t, err)
		test.EqOp(t, ContentTypeJSON, ct)
	})

	T.Run("rejects an unknown content type instead of defaulting to JSON", func(t *testing.T) {
		t.Parallel()

		_, err := NewContentType(Config{ContentType: "application/protobuf"})
		test.ErrorIs(t, err, ErrUnsupportedContentType)
	})
}
