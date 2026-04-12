package noop

import (
	"context"
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestNewEventReporter(T *testing.T) {
	T.Parallel()

	T.Run("returns non-nil reporter", func(t *testing.T) {
		t.Parallel()

		r := NewEventReporter()
		must.NotNil(t, r)
	})
}

func TestEventReporter_Close(T *testing.T) {
	T.Parallel()

	T.Run("does not panic", func(t *testing.T) {
		t.Parallel()

		r := NewEventReporter()
		test.NotPanic(t, func() {
			r.Close()
		})
	})
}

func TestEventReporter_AddUser(T *testing.T) {
	T.Parallel()

	T.Run("returns nil", func(t *testing.T) {
		t.Parallel()

		r := NewEventReporter()
		err := r.AddUser(context.Background(), "user123", map[string]any{"key": "value"})
		test.NoError(t, err)
	})
}

func TestEventReporter_EventOccurred(T *testing.T) {
	T.Parallel()

	T.Run("returns nil", func(t *testing.T) {
		t.Parallel()

		r := NewEventReporter()
		err := r.EventOccurred(context.Background(), "event_name", "user123", map[string]any{"key": "value"})
		test.NoError(t, err)
	})
}

func TestEventReporter_EventOccurredAnonymous(T *testing.T) {
	T.Parallel()

	T.Run("returns nil", func(t *testing.T) {
		t.Parallel()

		r := NewEventReporter()
		err := r.EventOccurredAnonymous(context.Background(), "event_name", "anon123", map[string]any{"key": "value"})
		test.NoError(t, err)
	})
}
