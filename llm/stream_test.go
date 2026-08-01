package llm

import (
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestNewSliceStream(T *testing.T) {
	T.Parallel()

	T.Run("yields every event in order", func(t *testing.T) {
		t.Parallel()

		stream := NewSliceStream(
			Event{Type: EventTextDelta, Text: "one"},
			Event{Type: EventTextDelta, Text: "two"},
			Event{Type: EventDone, StopReason: StopReasonEndTurn},
		)
		t.Cleanup(func() { must.NoError(t, stream.Close()) })

		var seen []Event
		for stream.Next() {
			seen = append(seen, stream.Current())
		}

		must.NoError(t, stream.Err())
		must.SliceLen(t, 3, seen)
		test.EqOp(t, "one", seen[0].Text)
		test.EqOp(t, "two", seen[1].Text)
		test.EqOp(t, EventDone, seen[2].Type)
	})

	T.Run("with no events", func(t *testing.T) {
		t.Parallel()

		stream := NewSliceStream()

		test.False(t, stream.Next())
		must.NoError(t, stream.Err())
		must.NoError(t, stream.Close())
	})

	T.Run("stops yielding once closed", func(t *testing.T) {
		t.Parallel()

		stream := NewSliceStream(
			Event{Type: EventTextDelta, Text: "one"},
			Event{Type: EventTextDelta, Text: "two"},
		)

		must.True(t, stream.Next())
		test.EqOp(t, "one", stream.Current().Text)

		must.NoError(t, stream.Close())
		test.False(t, stream.Next())

		// Current keeps the last event it yielded rather than zeroing.
		test.EqOp(t, "one", stream.Current().Text)
	})

	T.Run("closing twice is safe", func(t *testing.T) {
		t.Parallel()

		stream := NewSliceStream()

		must.NoError(t, stream.Close())
		must.NoError(t, stream.Close())
	})
}
