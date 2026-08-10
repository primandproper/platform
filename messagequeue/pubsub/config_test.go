package pubsub

import (
	"testing"

	"github.com/shoenig/test"
)

func TestConfig_ValidateWithContext(T *testing.T) {
	T.Parallel()

	T.Run("with a project ID", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{ProjectID: t.Name()}

		test.NoError(t, cfg.ValidateWithContext(t.Context()))
	})

	// The Pub/Sub client cannot be built without one, so an empty config was
	// only ever a construction error waiting to happen.
	T.Run("without a project ID", func(t *testing.T) {
		t.Parallel()

		test.Error(t, (&Config{}).ValidateWithContext(t.Context()))
	})
}
