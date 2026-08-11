package oteltrace

import (
	"testing"

	"github.com/shoenig/test"
)

func TestConfig_SetupOtelHTTP(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		cfg := &Config{
			CollectorEndpoint: "blah blah blah",
		}

		actual, err := SetupOtelGRPC(ctx, t.Name(), 0, cfg)
		test.NoError(t, err)
		test.NotNil(t, actual)
	})
}
