package tracing

import (
	"testing"

	"github.com/shoenig/test"
)

func TestGetCallerName(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		test.NotEq(t, "", GetCallerName())
	})
}
