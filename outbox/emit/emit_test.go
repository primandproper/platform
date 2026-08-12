package outboxemit

import (
	"context"
	"testing"

	"github.com/primandproper/platform-go/v10/database"
	platformerrors "github.com/primandproper/platform-go/v10/errors"
	nooplogging "github.com/primandproper/platform-go/v10/observability/logging/noop"
	noopmetrics "github.com/primandproper/platform-go/v10/observability/metrics/noop"
	nooptracing "github.com/primandproper/platform-go/v10/observability/tracing/noop"
	"github.com/primandproper/platform-go/v10/outbox"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestNewEmitter(T *testing.T) {
	T.Parallel()

	T.Run("builds from a topic and an enqueuer", func(t *testing.T) {
		t.Parallel()

		e, err := NewEmitter[dataChange]("data_changes", &recordingEnqueuer{})
		must.NoError(t, err)
		must.NotNil(t, e)
		test.EqOp(t, "data_changes", e.Topic())
	})

	T.Run("accepts every observability option", func(t *testing.T) {
		t.Parallel()

		e, err := NewEmitter[dataChange]("data_changes", &recordingEnqueuer{},
			WithLogger(nooplogging.NewLogger()),
			WithTracerProvider(nooptracing.NewTracerProvider()),
			WithMetricsProvider(noopmetrics.NewMetricsProvider()),
		)
		must.NoError(t, err)
		must.NotNil(t, e)
	})

	T.Run("skips nil options", func(t *testing.T) {
		t.Parallel()

		e, err := NewEmitter[dataChange]("data_changes", &recordingEnqueuer{}, nil)
		must.NoError(t, err)
		must.NotNil(t, e)
	})

	T.Run("rejects an empty topic", func(t *testing.T) {
		t.Parallel()

		_, err := NewEmitter[dataChange]("", &recordingEnqueuer{})
		test.ErrorIs(t, err, outbox.ErrEmptyTopic)
	})

	T.Run("rejects a nil enqueuer", func(t *testing.T) {
		t.Parallel()

		_, err := NewEmitter[dataChange]("data_changes", nil)
		test.ErrorIs(t, err, ErrNilEnqueuer)
		test.ErrorIs(t, err, platformerrors.ErrNilInputParameter)
	})
}

func TestNewEmitter_sideEffects(T *testing.T) {
	T.Parallel()

	noop := func(context.Context, database.SQLQueryExecutor, dataChange) ([]outbox.Message, error) {
		return nil, nil
	}

	T.Run("registers a side effect", func(t *testing.T) {
		t.Parallel()

		e, err := NewEmitter[dataChange]("data_changes", &recordingEnqueuer{},
			WithSideEffect("webhooks", noop))
		must.NoError(t, err)
		must.SliceLen(t, 1, e.sideEffects)
	})

	T.Run("rejects an unnamed side effect", func(t *testing.T) {
		t.Parallel()

		_, err := NewEmitter[dataChange]("data_changes", &recordingEnqueuer{},
			WithSideEffect("", noop))
		test.ErrorIs(t, err, ErrEmptySideEffectName)
	})

	// Two different side effects, one name — which is the case worth refusing,
	// since each would then report as the other.
	T.Run("rejects two side effects under one name", func(t *testing.T) {
		t.Parallel()

		_, err := NewEmitter[dataChange]("data_changes", &recordingEnqueuer{},
			WithSideEffect("webhooks", noop),
			WithSideEffect("webhooks", func(context.Context, database.SQLQueryExecutor, dataChange) ([]outbox.Message, error) {
				return []outbox.Message{{Topic: "webhooks", Payload: "dispatch"}}, nil
			}))
		test.ErrorIs(t, err, ErrDuplicateSideEffect)
	})

	T.Run("rejects a nil side effect", func(t *testing.T) {
		t.Parallel()

		_, err := NewEmitter[dataChange]("data_changes", &recordingEnqueuer{},
			WithSideEffect("webhooks", SideEffect[dataChange](nil)))
		test.ErrorIs(t, err, ErrNilSideEffect)
	})

	// Option carries no type parameter, so this is the compiler's blind spot
	// and the reason NewEmitter narrows rather than trusting.
	T.Run("rejects a side effect for another message type", func(t *testing.T) {
		t.Parallel()

		_, err := NewEmitter[dataChange]("data_changes", &recordingEnqueuer{},
			WithSideEffect("webhooks", func(context.Context, database.SQLQueryExecutor, otherMessage) ([]outbox.Message, error) {
				return nil, nil
			}))
		test.ErrorIs(t, err, ErrSideEffectTypeMismatch)
	})
}

func TestEmitter_Emit(T *testing.T) {
	T.Parallel()

	msg := dataChange{Type: "setting.updated", ID: "setting-1"}

	T.Run("enqueues the caller's message on the emitter's topic", func(t *testing.T) {
		t.Parallel()

		rec := &recordingEnqueuer{}
		e, err := NewEmitter[dataChange]("data_changes", rec)
		must.NoError(t, err)

		must.NoError(t, e.Emit(t.Context(), testExecutor(), msg))

		msgs, calls := rec.recorded()
		test.EqOp(t, 1, calls)
		must.SliceLen(t, 1, msgs)
		test.EqOp(t, "data_changes", msgs[0].Topic)
		test.EqOp(t, "", msgs[0].Key)
		test.EqOp(t, msg, msgs[0].Payload.(dataChange))
	})

	T.Run("rejects a nil executor", func(t *testing.T) {
		t.Parallel()

		rec := &recordingEnqueuer{}
		e, err := NewEmitter[dataChange]("data_changes", rec)
		must.NoError(t, err)

		test.ErrorIs(t, e.Emit(t.Context(), nil, msg), outbox.ErrNilExecutor)

		_, calls := rec.recorded()
		test.EqOp(t, 0, calls)
	})

	T.Run("skips nil options", func(t *testing.T) {
		t.Parallel()

		rec := &recordingEnqueuer{}
		e, err := NewEmitter[dataChange]("data_changes", rec)
		must.NoError(t, err)

		must.NoError(t, e.Emit(t.Context(), testExecutor(), msg, nil))

		msgs, _ := rec.recorded()
		test.SliceLen(t, 1, msgs)
	})

	T.Run("surfaces an enqueue failure", func(t *testing.T) {
		t.Parallel()

		sentinel := platformerrors.New("no")
		e, err := NewEmitter[dataChange]("data_changes", &recordingEnqueuer{err: sentinel})
		must.NoError(t, err)

		test.ErrorIs(t, e.Emit(t.Context(), testExecutor(), msg), sentinel)
	})
}

// Everything one write owes goes in a single Enqueue, which is what keeps a
// three-event transaction to one round trip.
func TestEmitter_Emit_oneRoundTrip(T *testing.T) {
	T.Parallel()

	T.Run("one call however many messages", func(t *testing.T) {
		t.Parallel()

		rec := &recordingEnqueuer{}
		e, err := NewEmitter[dataChange]("data_changes", rec,
			WithSideEffect("webhooks", func(context.Context, database.SQLQueryExecutor, dataChange) ([]outbox.Message, error) {
				return []outbox.Message{{Topic: "webhooks", Payload: "dispatch"}}, nil
			}))
		must.NoError(t, err)

		must.NoError(t, e.Emit(t.Context(), testExecutor(), dataChange{ID: "setting-1"},
			WithIndexUpsert("settings-index", "setting-1")))

		msgs, calls := rec.recorded()
		test.EqOp(t, 1, calls)
		must.SliceLen(t, 3, msgs)

		// The caller's own message first, then the options in the order given,
		// then the registered side effects.
		test.EqOp(t, "data_changes", msgs[0].Topic)
		test.EqOp(t, "settings-index", msgs[1].Topic)
		test.EqOp(t, "webhooks", msgs[2].Topic)
	})
}

func TestEmitter_Emit_sideEffects(T *testing.T) {
	T.Parallel()

	T.Run("hands the side effect the executor and the message", func(t *testing.T) {
		t.Parallel()

		var (
			gotExecutor database.SQLQueryExecutor
			gotMessage  dataChange
		)

		executor := testExecutor()
		rec := &recordingEnqueuer{}

		e, err := NewEmitter[dataChange]("data_changes", rec,
			WithSideEffect("webhooks", func(_ context.Context, q database.SQLQueryExecutor, m dataChange) ([]outbox.Message, error) {
				gotExecutor, gotMessage = q, m

				return nil, nil
			}))
		must.NoError(t, err)

		msg := dataChange{Type: "setting.updated", ID: "setting-1"}
		must.NoError(t, e.Emit(t.Context(), executor, msg))

		test.EqOp(t, msg, gotMessage)
		must.NotNil(t, gotExecutor)
		test.True(t, gotExecutor == executor)
	})

	T.Run("runs them in registration order", func(t *testing.T) {
		t.Parallel()

		var order []string

		record := func(name string) SideEffect[dataChange] {
			return func(context.Context, database.SQLQueryExecutor, dataChange) ([]outbox.Message, error) {
				order = append(order, name)

				return nil, nil
			}
		}

		e, err := NewEmitter[dataChange]("data_changes", &recordingEnqueuer{},
			WithSideEffect("first", record("first")),
			WithSideEffect("second", record("second")))
		must.NoError(t, err)

		must.NoError(t, e.Emit(t.Context(), testExecutor(), dataChange{ID: "setting-1"}))

		test.Eq(t, []string{"first", "second"}, order)
	})

	// A side effect that cannot record what it owes must take the transaction
	// down with it, rather than letting the row change commit alone.
	T.Run("a failure aborts before anything is enqueued", func(t *testing.T) {
		t.Parallel()

		sentinel := platformerrors.New("no endpoints table")
		rec := &recordingEnqueuer{}

		e, err := NewEmitter[dataChange]("data_changes", rec,
			WithSideEffect("webhooks", func(context.Context, database.SQLQueryExecutor, dataChange) ([]outbox.Message, error) {
				return nil, sentinel
			}))
		must.NoError(t, err)

		test.ErrorIs(t, e.Emit(t.Context(), testExecutor(), dataChange{ID: "setting-1"}), sentinel)

		_, calls := rec.recorded()
		test.EqOp(t, 0, calls)
	})
}
