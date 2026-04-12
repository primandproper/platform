package objectstorage

import (
	"testing"

	"github.com/shoenig/test"
)

func TestGCPConfig_ValidateWithContext(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		cfg := &GCPConfig{
			BucketName: t.Name(),
		}

		test.NoError(t, cfg.ValidateWithContext(ctx))
	})

	T.Run("with missing bucket name", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		cfg := &GCPConfig{}

		test.Error(t, cfg.ValidateWithContext(ctx))
	})
}
