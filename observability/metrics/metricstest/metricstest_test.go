package metricstest

import (
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestInt64Counter(T *testing.T) {
	T.Parallel()

	T.Run("returns a counter", func(t *testing.T) {
		t.Parallel()

		test.NotNil(t, Int64Counter(t, "test_counter"))
	})
}

func TestFloat64Histogram(T *testing.T) {
	T.Parallel()

	T.Run("returns a usable histogram", func(t *testing.T) {
		t.Parallel()

		h := Float64Histogram(t, t.Name())
		must.NotNil(t, h)

		// Recording must not panic — the point of the helper is a histogram a
		// test can hand to production code without standing up a meter provider.
		h.Record(t.Context(), 1.5)
	})
}
