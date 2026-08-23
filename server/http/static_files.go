package http

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// RootLevelAssetsHandler returns an http.HandlerFunc that serves static files from assetsDir.
// It only serves root-level files (no subdirectories) and guards against path traversal.
// Register with router.Get("/*", RootLevelAssetsHandler(assetsDir)) as the last route.
func RootLevelAssetsHandler(assetsDir string) http.HandlerFunc {
	fileServer := http.FileServer(http.Dir(assetsDir))

	return func(w http.ResponseWriter, r *http.Request) {
		// Only serve root-level files (no subdirectories)
		if strings.Contains(r.URL.Path[1:], "/") {
			http.NotFound(w, r)
			return
		}

		// Check if the file exists in the assets directory (guard against path traversal)
		filePath := filepath.Clean(filepath.Join(assetsDir, r.URL.Path))
		// Every failure to resolve a path answers 404, the same as a path that
		// resolves cleanly to nothing. The alternative — reporting why — describes
		// the server's filesystem to whoever asked for the file.
		absAssets, err := filepath.Abs(assetsDir)
		if err != nil {
			http.NotFound(w, r)
			return
		}

		absFilePath, err := filepath.Abs(filePath)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		rel, err := filepath.Rel(absAssets, absFilePath)
		if err != nil || strings.HasPrefix(rel, "..") {
			http.NotFound(w, r)
			return
		}

		// The path is validated above to be within assetsDir.
		info, statErr := os.Stat(filePath)
		if statErr == nil && !info.IsDir() {
			fileServer.ServeHTTP(w, r)
			return
		}

		http.NotFound(w, r)
	}
}
