package jsonl

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shoenig/test/must"
)

// benchRecord carries a sizable payload so the sweep below crosses the
// bufio.Writer's default 4KiB buffer: small records coalesce into one write
// syscall, large ones do not.
type benchRecord struct {
	Name    string `json:"name"`
	Payload string `json:"payload"`
	Seq     int    `json:"seq"`
}

// newBenchSink opens a sink under a temp dir with a rotation threshold no
// benchmark run will reach, keeping rename-and-prune out of the measurement.
// Rotation itself is not benchmarked: it happens once per MaxBytes written
// (64MiB by default), so it is not a per-record cost.
func newBenchSink(b *testing.B) *Sink {
	b.Helper()

	path := filepath.Join(b.TempDir(), "capture.jsonl")

	s, err := NewSink(&Config{Path: path, MaxBytes: 1 << 40, MaxFiles: DefaultMaxFiles})
	must.NoError(b, err)
	b.Cleanup(func() { _ = s.Close() })

	return s
}

// BenchmarkSink_Write measures the sink's per-record cost: a json.Marshal, a
// newline append, and a buffered write under the sink's mutex. This runs in
// the Recorder's flusher goroutine rather than on a request, so it does not
// bound request latency — it bounds how many events per second the capture
// pipeline can absorb before the Recorder's buffer starts dropping.
func BenchmarkSink_Write(b *testing.B) {
	for _, size := range []int{16, 256, 4096} {
		rec := &benchRecord{
			Name:    "bench",
			Payload: strings.Repeat("a", size),
			Seq:     1,
		}

		b.Run(fmt.Sprintf("%dB", size), func(b *testing.B) {
			s := newBenchSink(b)

			for b.Loop() {
				_ = s.Write(rec)
			}
		})
	}
}
