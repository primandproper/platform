package noop

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestNewMediaUploadProcessor(T *testing.T) {
	T.Parallel()

	T.Run("returns non-nil processor", func(t *testing.T) {
		t.Parallel()

		p := NewMediaUploadProcessor()
		must.NotNil(t, p)
	})
}

func TestMediaUploadProcessor_ProcessFile(T *testing.T) {
	T.Parallel()

	T.Run("returns empty upload and no error", func(t *testing.T) {
		t.Parallel()

		p := NewMediaUploadProcessor()
		r := httptest.NewRequest(http.MethodPost, "/", http.NoBody)

		upload, err := p.ProcessFile(t.Context(), r, "avatar")

		must.NoError(t, err)
		must.NotNil(t, upload)
	})
}

func TestMediaUploadProcessor_ProcessFiles(T *testing.T) {
	T.Parallel()

	T.Run("returns empty slice and no error", func(t *testing.T) {
		t.Parallel()

		p := NewMediaUploadProcessor()
		r := httptest.NewRequest(http.MethodPost, "/", http.NoBody)

		uploads, err := p.ProcessFiles(t.Context(), r, "photos")

		must.NoError(t, err)
		test.SliceEmpty(t, uploads)
		test.NotNil(t, uploads)
	})
}
