package jsonl

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	clockfake "github.com/primandproper/platform-go/v7/clock/fake"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

type record struct {
	Name string `json:"name"`
	Seq  int    `json:"seq"`
}

func newTestSink(t *testing.T, maxBytes int64, maxFiles int) (*Sink, string, *clockfake.Clock) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "capture.jsonl")
	fc := clockfake.New(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC))

	s, err := NewSink(&Config{Path: path, MaxBytes: maxBytes, MaxFiles: maxFiles}, WithClock(fc))
	must.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	return s, path, fc
}

func readLines(t *testing.T, path string) []record {
	t.Helper()

	f, err := os.Open(path)
	must.NoError(t, err)
	defer func() { _ = f.Close() }()

	var out []record
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var r record
		must.NoError(t, json.Unmarshal(scanner.Bytes(), &r))
		out = append(out, r)
	}
	must.NoError(t, scanner.Err())

	return out
}

func rotatedSiblings(t *testing.T, path string) []string {
	t.Helper()

	rotated, err := filepath.Glob(path + ".*")
	must.NoError(t, err)

	return rotated
}

func TestNewSink(T *testing.T) {
	T.Parallel()

	T.Run("rejects nil config and empty path", func(t *testing.T) {
		t.Parallel()

		_, err := NewSink(nil)
		test.Error(t, err)

		_, err = NewSink(&Config{})
		test.Error(t, err)
	})

	T.Run("creates parent directories", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "nested", "deeper", "capture.jsonl")
		s, err := NewSink(&Config{Path: path})
		must.NoError(t, err)
		must.NoError(t, s.Close())
	})
}

func TestSink_WriteAndFlush(T *testing.T) {
	T.Parallel()

	T.Run("records land one JSON line each", func(t *testing.T) {
		t.Parallel()

		s, path, _ := newTestSink(t, DefaultMaxBytes, DefaultMaxFiles)

		must.NoError(t, s.Write(&record{Name: "a", Seq: 1}))
		must.NoError(t, s.Write(&record{Name: "b", Seq: 2}))
		must.NoError(t, s.Flush())

		lines := readLines(t, path)
		must.SliceLen(t, 2, lines)
		test.EqOp(t, "a", lines[0].Name)
		test.EqOp(t, 2, lines[1].Seq)
	})

	T.Run("writing to a closed sink errors", func(t *testing.T) {
		t.Parallel()

		s, _, _ := newTestSink(t, DefaultMaxBytes, DefaultMaxFiles)

		must.NoError(t, s.Close())
		// A second Close is a no-op.
		must.NoError(t, s.Close())

		test.Error(t, s.Write(&record{Name: "late"}))
	})
}

func TestSink_Rotation(T *testing.T) {
	T.Parallel()

	T.Run("rotates by size and prunes to MaxFiles", func(t *testing.T) {
		t.Parallel()

		// Tiny threshold: every second write rotates.
		s, path, fc := newTestSink(t, 40, 2)

		for seq := range 8 {
			must.NoError(t, s.Write(&record{Name: "rotate-me", Seq: seq}))
			// Distinct stamps for distinct rotations.
			fc.Advance(time.Second)
		}
		must.NoError(t, s.Flush())

		rotated := rotatedSiblings(t, path)
		test.SliceLen(t, 2, rotated)

		// The live file still has the newest record.
		lines := readLines(t, path)
		must.SliceLen(t, 1, lines)
		test.EqOp(t, 7, lines[0].Seq)
	})

	T.Run("an oversized record is written whole to a fresh file", func(t *testing.T) {
		t.Parallel()

		s, path, fc := newTestSink(t, 40, 4)

		must.NoError(t, s.Write(&record{Name: "small", Seq: 1}))
		fc.Advance(time.Second)

		big := record{Name: string(make([]byte, 200)), Seq: 2}
		must.NoError(t, s.Write(&big))
		must.NoError(t, s.Flush())

		// The small record rotated aside; the oversized one lives alone.
		test.SliceLen(t, 1, rotatedSiblings(t, path))
		lines := readLines(t, path)
		must.SliceLen(t, 1, lines)
		test.EqOp(t, 2, lines[0].Seq)
	})

	T.Run("byte count resumes across a reopen", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "capture.jsonl")
		fc := clockfake.New(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC))

		first, err := NewSink(&Config{Path: path, MaxBytes: 60, MaxFiles: 4}, WithClock(fc))
		must.NoError(t, err)
		must.NoError(t, first.Write(&record{Name: "before-restart", Seq: 1}))
		must.NoError(t, first.Close())

		// A new sink over the same path inherits the existing bytes, so this
		// write pushes past MaxBytes and rotates rather than growing unbounded.
		second, err := NewSink(&Config{Path: path, MaxBytes: 60, MaxFiles: 4}, WithClock(fc))
		must.NoError(t, err)
		t.Cleanup(func() { _ = second.Close() })

		must.NoError(t, second.Write(&record{Name: "after-restart", Seq: 2}))
		must.NoError(t, second.Flush())

		test.SliceLen(t, 1, rotatedSiblings(t, path))
		lines := readLines(t, path)
		must.SliceLen(t, 1, lines)
		test.EqOp(t, 2, lines[0].Seq)
	})
}
