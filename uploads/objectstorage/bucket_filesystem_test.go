package objectstorage

import (
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
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

func TestDirectoryMode_UnmarshalText(T *testing.T) {
	T.Parallel()

	// The bug: os.FileMode is a uint32, so every decoder read DIRECTORY_MODE=0700
	// in base 10 — decimal 700 is 0o1274, the sticky bit plus a permission set
	// nobody asked for.
	T.Run("parses octal with and without a prefix", func(t *testing.T) {
		t.Parallel()

		for _, raw := range []string{"0700", "700", "0o700"} {
			var m DirectoryMode
			must.NoError(t, m.UnmarshalText([]byte(raw)), must.Sprintf("input %q", raw))
			test.EqOp(t, DirectoryMode(0o700), m, test.Sprintf("input %q", raw))
		}
	})

	T.Run("rejects a non-octal value", func(t *testing.T) {
		t.Parallel()

		var m DirectoryMode
		test.Error(t, m.UnmarshalText([]byte("0800")))
	})

	T.Run("round trips through MarshalText", func(t *testing.T) {
		t.Parallel()

		out, err := DirectoryMode(0o750).MarshalText()
		must.NoError(t, err)

		var m DirectoryMode
		must.NoError(t, m.UnmarshalText(out))
		test.EqOp(t, DirectoryMode(0o750), m)
	})
}
