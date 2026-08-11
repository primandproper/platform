package httpmw

import (
	"testing"

	"github.com/shoenig/test"
)

func TestIsHealthCheck(T *testing.T) {
	T.Parallel()

	T.Run("probe paths are health checks", func(t *testing.T) {
		t.Parallel()

		for _, path := range []string{"/_ops_/live", "/_ops_/ready", "/healthz", "/readyz"} {
			test.True(t, IsHealthCheck(path), test.Sprintf("path %q", path))
		}
	})

	T.Run("other operational paths are not health checks", func(t *testing.T) {
		t.Parallel()

		// Still worth a log line, which is what separates this from IsUntraced.
		test.False(t, IsHealthCheck("/_ops_/version"))
	})

	T.Run("ordinary paths are not health checks", func(t *testing.T) {
		t.Parallel()

		test.False(t, IsHealthCheck("/api/v1/things"))
	})
}

func TestIsUntraced(T *testing.T) {
	T.Parallel()

	T.Run("every operational path is untraced", func(t *testing.T) {
		t.Parallel()

		for _, path := range []string{"/_ops_/live", "/_ops_/ready", "/_ops_/version", "/_ops_/health"} {
			test.True(t, IsUntraced(path), test.Sprintf("path %q", path))
		}
	})

	T.Run("the bare probe aliases are untraced", func(t *testing.T) {
		t.Parallel()

		test.True(t, IsUntraced("/healthz"))
		test.True(t, IsUntraced("/readyz"))
	})

	T.Run("the apple site association file is untraced", func(t *testing.T) {
		t.Parallel()

		test.True(t, IsUntraced("/.well-known/apple-app-site-association"))
	})

	T.Run("ordinary paths are traced", func(t *testing.T) {
		t.Parallel()

		test.False(t, IsUntraced("/api/v1/things"))
		test.False(t, IsUntraced("/"))
	})
}
