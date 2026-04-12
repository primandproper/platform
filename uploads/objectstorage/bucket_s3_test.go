package objectstorage

import (
	"testing"

	"github.com/shoenig/test"
)

func TestS3Config_ValidateWithContext(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		cfg := &S3Config{
			BucketName: t.Name(),
		}

		test.NoError(t, cfg.ValidateWithContext(ctx))
	})

	T.Run("with missing bucket name", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		cfg := &S3Config{}

		test.Error(t, cfg.ValidateWithContext(ctx))
	})
}
