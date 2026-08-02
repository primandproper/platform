package metricstest

import (
	"testing"

	"github.com/shoenig/test"
)

func TestInt64Counter(T *testing.T) {
	T.Parallel()

	T.Run("returns a counter", func(t *testing.T) {
		t.Parallel()

		test.NotNil(t, Int64Counter(t, "test_counter"))
	})
}
