package outboxemit

import (
	"errors"
	"testing"

	"github.com/primandproper/platform-go/v10/clock"
	platformerrors "github.com/primandproper/platform-go/v10/errors"
	"github.com/primandproper/platform-go/v10/observability/logging"
	nooplogging "github.com/primandproper/platform-go/v10/observability/logging/noop"
	noopmetrics "github.com/primandproper/platform-go/v10/observability/metrics/noop"
	nooptracing "github.com/primandproper/platform-go/v10/observability/tracing/noop"
	"github.com/primandproper/platform-go/v10/outbox"
	searchsync "github.com/primandproper/platform-go/v10/search/sync"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestNewOptions(T *testing.T) {
	T.Parallel()

	T.Run("defaults to a working clock and nothing else", func(t *testing.T) {
		t.Parallel()

		o := newOptions()
		must.NotNil(t, o.clock)
		test.Nil(t, o.logger)
		test.Nil(t, o.tracerProvider)
		test.Nil(t, o.metricsProvider)
		test.SliceEmpty(t, o.sideEffects)
	})
}

func TestOptions(T *testing.T) {
	T.Parallel()

	T.Run("apply what they are given", func(t *testing.T) {
		t.Parallel()

		var logger logging.Logger = nooplogging.NewLogger()
		tracerProvider := nooptracing.NewTracerProvider()
		metricsProvider := noopmetrics.NewMetricsProvider()
		var c clock.Clock = clock.NewClock()

		o := newOptions()
		for _, opt := range []Option{
			WithLogger(logger),
			WithTracerProvider(tracerProvider),
			WithMetricsProvider(metricsProvider),
			WithClock(c),
		} {
			opt(o)
		}

		test.EqOp(t, logger, o.logger)
		test.EqOp(t, tracerProvider, o.tracerProvider)
		test.EqOp(t, metricsProvider, o.metricsProvider)
		test.EqOp(t, c, o.clock)
	})

	T.Run("keep the default clock when given a nil one", func(t *testing.T) {
		t.Parallel()

		o := newOptions()
		WithClock(nil)(o)

		must.NotNil(t, o.clock)
	})
}

// emit runs one Emit through a recording enqueuer and returns what it produced,
// which is what every option below is judged on.
func emit(t *testing.T, opts ...EmitOption) ([]outbox.Message, error) {
	t.Helper()

	rec := &recordingEnqueuer{}

	e, err := NewEmitter[dataChange]("data_changes", rec)
	must.NoError(t, err)

	if err = e.Emit(t.Context(), testExecutor(), dataChange{ID: "setting-1"}, opts...); err != nil {
		_, calls := rec.recorded()
		test.EqOp(t, 0, calls)

		return nil, err
	}

	msgs, calls := rec.recorded()
	test.EqOp(t, 1, calls)

	return msgs, nil
}

func TestWithOrderingKey(T *testing.T) {
	T.Parallel()

	T.Run("keys the caller's own message", func(t *testing.T) {
		t.Parallel()

		msgs, err := emit(t, WithOrderingKey("setting-1"))
		must.NoError(t, err)
		must.SliceLen(t, 1, msgs)
		test.EqOp(t, "setting-1", msgs[0].Key)
	})

	// Unkeyed is reached by not passing the option; passing it empty is a
	// caller whose ID came back blank, which has no other way to be noticed.
	T.Run("refuses an empty key", func(t *testing.T) {
		t.Parallel()

		_, err := emit(t, WithOrderingKey(""))
		test.ErrorIs(t, err, ErrEmptyOrderingKey)
		test.ErrorIs(t, err, platformerrors.ErrEmptyInputParameter)
	})
}

func TestWithIndexUpsert(T *testing.T) {
	T.Parallel()

	T.Run("adds an upsert event naming the document", func(t *testing.T) {
		t.Parallel()

		msgs, err := emit(t, WithIndexUpsert("settings-index", "setting-1"))
		must.NoError(t, err)
		must.SliceLen(t, 2, msgs)

		test.EqOp(t, "settings-index", msgs[1].Topic)

		event, ok := msgs[1].Payload.(searchsync.Event)
		must.True(t, ok)
		test.EqOp(t, searchsync.OpUpsert, event.Op)
		test.EqOp(t, "setting-1", event.DocumentID)
		test.False(t, event.OccurredAt.IsZero())
	})

	// The convention this package exists to stop anybody having to remember.
	T.Run("keys the event by document ID", func(t *testing.T) {
		t.Parallel()

		msgs, err := emit(t, WithIndexUpsert("settings-index", "setting-1"))
		must.NoError(t, err)
		must.SliceLen(t, 2, msgs)
		test.EqOp(t, "setting-1", msgs[1].Key)
	})

	// The event's key is the document's, independently of the caller's own.
	T.Run("does not take the caller's ordering key", func(t *testing.T) {
		t.Parallel()

		msgs, err := emit(t,
			WithOrderingKey("account-7"),
			WithIndexUpsert("settings-index", "setting-1"))
		must.NoError(t, err)
		must.SliceLen(t, 2, msgs)
		test.EqOp(t, "account-7", msgs[0].Key)
		test.EqOp(t, "setting-1", msgs[1].Key)
	})

	T.Run("refuses an event with no topic", func(t *testing.T) {
		t.Parallel()

		_, err := emit(t, WithIndexUpsert("", "setting-1"))
		test.ErrorIs(t, err, outbox.ErrEmptyTopic)
	})

	T.Run("refuses an event with no document ID", func(t *testing.T) {
		t.Parallel()

		_, err := emit(t, WithIndexUpsert("settings-index", ""))
		test.ErrorIs(t, err, ErrEmptyDocumentID)
	})
}

func TestWithIndexDelete(T *testing.T) {
	T.Parallel()

	T.Run("adds a delete event naming the document", func(t *testing.T) {
		t.Parallel()

		msgs, err := emit(t, WithIndexDelete("settings-index", "setting-1"))
		must.NoError(t, err)
		must.SliceLen(t, 2, msgs)

		event, ok := msgs[1].Payload.(searchsync.Event)
		must.True(t, ok)
		test.EqOp(t, searchsync.OpDelete, event.Op)
		test.EqOp(t, "setting-1", event.DocumentID)
		test.EqOp(t, "setting-1", msgs[1].Key)
	})

	T.Run("refuses an event with no document ID", func(t *testing.T) {
		t.Parallel()

		_, err := emit(t, WithIndexDelete("settings-index", ""))
		test.ErrorIs(t, err, ErrEmptyDocumentID)
	})
}

// One write can touch two indexes, so the options accumulate rather than
// replacing one another.
func TestEmitOptions_accumulate(T *testing.T) {
	T.Parallel()

	T.Run("keeps every index event, in the order given", func(t *testing.T) {
		t.Parallel()

		msgs, err := emit(t,
			WithIndexUpsert("settings-index", "setting-1"),
			WithIndexDelete("archive-index", "setting-1"))
		must.NoError(t, err)
		must.SliceLen(t, 3, msgs)
		test.EqOp(t, "settings-index", msgs[1].Topic)
		test.EqOp(t, "archive-index", msgs[2].Topic)
	})

	// The first failure wins and the emission is refused whole: there is no
	// partial mode, because the point of one call is that its side effects are
	// not individually forgettable.
	T.Run("the first refusal stops the whole emission", func(t *testing.T) {
		t.Parallel()

		_, err := emit(t,
			WithIndexUpsert("settings-index", ""),
			WithOrderingKey(""))
		test.ErrorIs(t, err, ErrEmptyDocumentID)
		test.False(t, errors.Is(err, ErrEmptyOrderingKey))
	})
}

func TestWithMessages(T *testing.T) {
	T.Parallel()

	T.Run("adds messages verbatim", func(t *testing.T) {
		t.Parallel()

		msgs, err := emit(t, WithMessages(
			outbox.Message{Topic: "audit", Payload: "recorded", Key: "setting-1"},
			outbox.Message{Topic: "notifications", Payload: "sent"},
		))
		must.NoError(t, err)
		must.SliceLen(t, 3, msgs)
		test.EqOp(t, "audit", msgs[1].Topic)
		test.EqOp(t, "setting-1", msgs[1].Key)
		test.EqOp(t, "notifications", msgs[2].Topic)
	})

	T.Run("adds nothing when given nothing", func(t *testing.T) {
		t.Parallel()

		msgs, err := emit(t, WithMessages())
		must.NoError(t, err)
		test.SliceLen(t, 1, msgs)
	})
}
