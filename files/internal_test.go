package files

import (
	"os"
	"path/filepath"
	"testing"

	loggingnoop "github.com/primandproper/platform-go/v9/observability/logging/noop"
	tracingnoop "github.com/primandproper/platform-go/v9/observability/tracing/noop"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestCloseQuietly(T *testing.T) {
	T.Parallel()

	T.Run("logs and does not panic when Close fails", func(t *testing.T) {
		t.Parallel()

		r := newStandardReader(loggingnoop.NewLogger(), tracingnoop.NewTracerProvider())

		f, err := os.Create(filepath.Join(t.TempDir(), "f.txt"))
		must.NoError(t, err)
		must.NoError(t, f.Close()) // first close succeeds; the second will fail

		test.NotPanic(t, func() {
			r.closeQuietly(f) // double close returns an error, which closeQuietly logs
		})
	})

	// WithLogger is optional and documents that an absent logger logs nowhere.
	// closeQuietly logs through the retained field rather than through the
	// Observer, so nothing but normalizing at construction keeps that promise —
	// and the path only runs when a close has already failed, which is where a
	// panic would be least welcome and least likely to be noticed in testing.
	T.Run("does not panic when no logger was supplied", func(t *testing.T) {
		t.Parallel()

		r := NewReader().(*standardReader)
		must.NotNil(t, r.logger)

		f, err := os.Create(filepath.Join(t.TempDir(), "f.txt"))
		must.NoError(t, err)
		must.NoError(t, f.Close())

		test.NotPanic(t, func() {
			r.closeQuietly(f)
		})
	})
}
