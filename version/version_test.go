package version

import (
	"testing"

	"github.com/shoenig/test"
)

func TestGet(T *testing.T) { //nolint:paralleltest // mutates package-level vars; subtests must run sequentially
	T.Run("returns unknown when vars are unset", func(t *testing.T) { //nolint:paralleltest // mutates package-level vars; subtests must run sequentially
		origVersion, origHash, origCTime, origBTime := Version, CommitHash, CommitTime, BuildTime
		Version, CommitHash, CommitTime, BuildTime = "", "", "", ""
		t.Cleanup(func() {
			Version, CommitHash, CommitTime, BuildTime = origVersion, origHash, origCTime, origBTime
		})

		info := Get()
		test.EqOp(t, "unknown", info.Version)
		test.EqOp(t, "unknown", info.CommitHash)
		test.EqOp(t, "unknown", info.CommitTime)
		test.EqOp(t, "unknown", info.BuildTime)
	})

	T.Run("returns set values when vars are populated", func(t *testing.T) { //nolint:paralleltest // mutates package-level vars; subtests must run sequentially
		origVersion, origHash, origCTime, origBTime := Version, CommitHash, CommitTime, BuildTime
		Version = "v1.2.3"
		CommitHash = "abc123"
		CommitTime = "2026-01-01T00:00:00Z"
		BuildTime = "2026-01-02T00:00:00Z"
		t.Cleanup(func() {
			Version, CommitHash, CommitTime, BuildTime = origVersion, origHash, origCTime, origBTime
		})

		info := Get()
		test.EqOp(t, "v1.2.3", info.Version)
		test.EqOp(t, "abc123", info.CommitHash)
		test.EqOp(t, "2026-01-01T00:00:00Z", info.CommitTime)
		test.EqOp(t, "2026-01-02T00:00:00Z", info.BuildTime)
	})
}
