package shredding

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	messagequeuemock "github.com/primandproper/platform-go/v10/messagequeue/mock"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestNewQueueBroadcaster(T *testing.T) {
	T.Parallel()

	T.Run("refuses a nil publisher", func(t *testing.T) {
		t.Parallel()

		broadcaster, err := NewQueueBroadcaster(nil)
		test.Nil(t, broadcaster)
		test.ErrorIs(t, err, ErrNilPublisher)
	})

	T.Run("publishes the subject", func(t *testing.T) {
		t.Parallel()

		var published any

		publisher := &messagequeuemock.PublisherMock{
			PublishFunc: func(_ context.Context, data any) error {
				published = data

				return nil
			},
		}

		broadcaster, err := NewQueueBroadcaster(publisher)
		must.NoError(t, err)

		must.NoError(t, broadcaster.Broadcast(t.Context(), testSubject))
		test.Eq(t, any(testSubject), published)
	})

	T.Run("reports a publish failure", func(t *testing.T) {
		t.Parallel()

		sentinel := errors.New("bus is down")
		publisher := &messagequeuemock.PublisherMock{
			PublishFunc: func(context.Context, any) error { return sentinel },
		}

		broadcaster, err := NewQueueBroadcaster(publisher)
		must.NoError(t, err)

		test.ErrorIs(t, broadcaster.Broadcast(t.Context(), testSubject), sentinel)
	})

	T.Run("refuses a subject with no ID", func(t *testing.T) {
		t.Parallel()

		publisher := &messagequeuemock.PublisherMock{
			PublishFunc: func(context.Context, any) error { return nil },
		}

		broadcaster, err := NewQueueBroadcaster(publisher)
		must.NoError(t, err)

		test.ErrorIs(t, broadcaster.Broadcast(t.Context(), Subject{Type: "user"}), ErrEmptySubjectID)
	})
}

func TestInvalidationHandler(T *testing.T) {
	T.Parallel()

	T.Run("round-trips what a Broadcaster publishes", func(t *testing.T) {
		t.Parallel()

		var published any

		publisher := &messagequeuemock.PublisherMock{
			PublishFunc: func(_ context.Context, data any) error {
				published = data

				return nil
			},
		}

		broadcaster, err := NewQueueBroadcaster(publisher)
		must.NoError(t, err)
		must.NoError(t, broadcaster.Broadcast(t.Context(), testSubject))

		// The two halves have to agree about the wire shape or a shred is
		// announced to a fleet that silently cannot read it.
		encoded, err := json.Marshal(published)
		must.NoError(t, err)

		invalidator := &recordingInvalidator{}
		must.NoError(t, InvalidationHandler(invalidator)(t.Context(), encoded))

		test.SliceLen(t, 1, invalidator.subjects)
		test.EqOp(t, testSubject, invalidator.subjects[0])
	})

	T.Run("rejects a message that is not a subject", func(t *testing.T) {
		t.Parallel()

		invalidator := &recordingInvalidator{}

		test.Error(t, InvalidationHandler(invalidator)(t.Context(), []byte("{")))
		test.SliceEmpty(t, invalidator.subjects)
	})

	T.Run("rejects a subject with no ID", func(t *testing.T) {
		t.Parallel()

		invalidator := &recordingInvalidator{}

		test.ErrorIs(t,
			InvalidationHandler(invalidator)(t.Context(), []byte(`{"type":"user"}`)),
			ErrEmptySubjectID)
		test.SliceEmpty(t, invalidator.subjects)
	})
}

// recordingInvalidator captures what a handler dropped.
type recordingInvalidator struct {
	subjects []Subject
}

var _ Invalidator = (*recordingInvalidator)(nil)

func (r *recordingInvalidator) Invalidate(subject Subject) {
	r.subjects = append(r.subjects, subject)
}
