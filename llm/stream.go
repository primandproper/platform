package llm

// EventType discriminates the union in Event.
type EventType string

// The kinds of event a Stream yields.
const (
	// EventTextDelta is a fragment of the response text, in Event.Text.
	// Fragments arrive in order and concatenate to the full response.
	EventTextDelta EventType = "text_delta"
	// EventThinkingDelta is a fragment of the model's reasoning, in
	// Event.Text.
	EventThinkingDelta EventType = "thinking_delta"
	// EventToolUse is a complete tool call, in Event.ToolUse. It is never a
	// fragment: the arguments have been accumulated in full before the event
	// is yielded, whatever the underlying provider streamed.
	EventToolUse EventType = "tool_use"
	// EventDone is the final event, carrying Event.StopReason and, when the
	// provider reported one, Event.Usage. Exactly one is yielded per
	// successfully completed stream, and none when the stream fails.
	EventDone EventType = "done"
)

// Event is one item from a Stream. Type says which of the remaining fields is
// meaningful.
type Event struct {
	ToolUse    *ToolUse
	Usage      *Usage
	Type       EventType
	Text       string
	StopReason StopReason
}

// Stream is a completion arriving incrementally.
//
// It is an explicit iterator rather than an iter.Seq2 or a channel because it
// has to be closed. A stream holds an HTTP response body open, and a consumer
// that stops early — because it has the tool call it wanted, because its own
// caller cancelled — has to be able to release it. Close is also what makes
// abandonment cheap: a range-over-func has no way to say "I am done with this"
// beyond breaking, and a channel has no way to say it at all.
//
// The usage is the familiar bufio.Scanner shape:
//
//	stream, err := provider.Stream(ctx, req)
//	if err != nil {
//		return err
//	}
//	defer func() { _ = stream.Close() }()
//
//	for stream.Next() {
//		switch event := stream.Current(); event.Type {
//		case llm.EventTextDelta:
//			fmt.Print(event.Text)
//		case llm.EventToolUse:
//			calls = append(calls, *event.ToolUse)
//		}
//	}
//
//	return stream.Err()
//
// Next returning false means either the stream ended or it failed, and Err
// distinguishes them. Close is always safe to call, including after a failure
// and more than once. A Stream is not safe for concurrent use.
type Stream interface {
	// Next advances to the next event, reporting whether there is one.
	Next() bool
	// Current returns the event Next advanced to. It is only valid after Next
	// returned true.
	Current() Event
	// Err returns the error that stopped the stream, or nil if it ended
	// cleanly or is still running.
	Err() error
	// Close releases the stream's resources. It is idempotent.
	Close() error
}

var _ Stream = (*sliceStream)(nil)

// NewSliceStream returns a Stream that yields the given events and then stops.
// It is what a Provider with nothing to say returns, and it saves consumers'
// tests from standing up an HTTP server to exercise their event handling.
func NewSliceStream(events ...Event) Stream {
	return &sliceStream{events: events}
}

type sliceStream struct {
	current Event
	events  []Event
	idx     int
	closed  bool
}

func (s *sliceStream) Next() bool {
	if s.closed || s.idx >= len(s.events) {
		return false
	}

	s.current = s.events[s.idx]
	s.idx++

	return true
}

func (s *sliceStream) Current() Event {
	return s.current
}

func (*sliceStream) Err() error {
	return nil
}

func (s *sliceStream) Close() error {
	s.closed = true

	return nil
}
