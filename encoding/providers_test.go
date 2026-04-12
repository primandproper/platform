package encoding

import (
	"testing"

	"github.com/shoenig/test"
)

func TestProvideContentType(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, ContentTypeJSON, ProvideContentType(Config{ContentType: "application/json"}))
	})
}
