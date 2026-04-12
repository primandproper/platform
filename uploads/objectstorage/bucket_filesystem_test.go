package objectstorage

import (
	"testing"

	"github.com/shoenig/test"
)

func TestFilesystemConfig_ValidateWithContext(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		cfg := &FilesystemConfig{
			RootDirectory: t.Name(),
		}

		test.NoError(t, cfg.ValidateWithContext(ctx))
	})

	T.Run("with missing root directory", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		cfg := &FilesystemConfig{}

		test.Error(t, cfg.ValidateWithContext(ctx))
	})
}
