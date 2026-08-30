package noop

import (
	"testing"

	"github.com/primandproper/platform-go/v13/eventcapture"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestSink(T *testing.T) {
	T.Parallel()

	T.Run("accepts and discards every record", func(t *testing.T) {
		t.Parallel()

		// What a deployment with capture wired but disabled runs: nothing is
		// written anywhere, and nothing about that is an error the caller can
		// act on.
		var sink eventcapture.Sink = NewSink()
		must.NotNil(t, sink)

		test.NoError(t, sink.Write(map[string]any{"event": "signup"}))
		test.NoError(t, sink.Write(nil))
	})

	T.Run("flushes and closes without complaint", func(t *testing.T) {
		t.Parallel()

		sink := NewSink()

		test.NoError(t, sink.Flush())
		test.NoError(t, sink.Close())

		// Closing releases nothing, so closing twice is not a mistake here.
		test.NoError(t, sink.Close())
	})
}
