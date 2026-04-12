package http

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestRootLevelAssetsHandler(T *testing.T) {
	T.Parallel()

	T.Run("serves root-level file", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		err := os.WriteFile(filepath.Join(dir, "robots.txt"), []byte("User-agent: *"), 0o600)
		must.NoError(t, err)

		handler := RootLevelAssetsHandler(dir)
		req := httptest.NewRequest(http.MethodGet, "/robots.txt", http.NoBody)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		test.EqOp(t, http.StatusOK, w.Code)
		test.StrContains(t, w.Body.String(), "User-agent")
	})

	T.Run("returns 404 for subdirectory paths", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		handler := RootLevelAssetsHandler(dir)
		req := httptest.NewRequest(http.MethodGet, "/sub/file.txt", http.NoBody)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		test.EqOp(t, http.StatusNotFound, w.Code)
	})

	T.Run("returns 404 for nonexistent file", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		handler := RootLevelAssetsHandler(dir)
		req := httptest.NewRequest(http.MethodGet, "/nonexistent.txt", http.NoBody)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		test.EqOp(t, http.StatusNotFound, w.Code)
	})

	T.Run("returns 404 for directory", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		err := os.Mkdir(filepath.Join(dir, "subdir"), 0o755)
		must.NoError(t, err)

		handler := RootLevelAssetsHandler(dir)
		req := httptest.NewRequest(http.MethodGet, "/subdir", http.NoBody)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		test.EqOp(t, http.StatusNotFound, w.Code)
	})

	T.Run("blocks path traversal attempts", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		handler := RootLevelAssetsHandler(dir)
		req := httptest.NewRequest(http.MethodGet, "/../etc/passwd", http.NoBody)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		test.EqOp(t, http.StatusNotFound, w.Code)
	})

	T.Run("blocks single-segment traversal", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		handler := RootLevelAssetsHandler(dir)

		req := httptest.NewRequest(http.MethodGet, "/x", http.NoBody)
		req.URL.Path = "/.."
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		test.EqOp(t, http.StatusNotFound, w.Code)
	})
}
