package noop

import (
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestNewUploadManager(T *testing.T) {
	T.Parallel()

	T.Run("returns non-nil manager", func(t *testing.T) {
		t.Parallel()

		m := NewUploadManager()
		must.NotNil(t, m)
	})
}

func TestUploadManager_SaveFile(T *testing.T) {
	T.Parallel()

	T.Run("returns no error", func(t *testing.T) {
		t.Parallel()

		m := NewUploadManager()
		err := m.SaveFile(t.Context(), "path/to/file", []byte("content"))

		test.NoError(t, err)
	})
}

func TestUploadManager_ReadFile(T *testing.T) {
	T.Parallel()

	T.Run("returns empty bytes and no error", func(t *testing.T) {
		t.Parallel()

		m := NewUploadManager()
		data, err := m.ReadFile(t.Context(), "path/to/file")

		must.NoError(t, err)
		test.SliceEmpty(t, data)
		test.NotNil(t, data)
	})
}
