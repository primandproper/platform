package o11yutils

import (
	"context"
	"testing"

	"github.com/shoenig/test"
)

func TestMustOtelResource(T *testing.T) {
	T.Parallel()

	T.Run("with service name", func(t *testing.T) {
		t.Parallel()
		res := MustOtelResource(context.Background(), "test-service")
		test.NotNil(t, res)
	})

	T.Run("without service name", func(t *testing.T) {
		t.Parallel()
		res := MustOtelResource(context.Background(), "")
		test.NotNil(t, res)
	})
}
