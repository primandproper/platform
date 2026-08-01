package observability

// BeginOption seeds the Operation an Observer returns, before the caller's own
// code runs against it.
//
// It exists because the overwhelmingly common shape of an instrumented method is
// to begin an operation and immediately describe it:
//
//	ctx, op := w.o11y.Begin(ctx)
//	defer op.End()
//
//	op.SetValues(map[string]any{
//		requestIDKey: req.ID,
//		statusKey:    string(req.Status),
//	})
//
// which separates the values from the operation they describe by a statement
// that must sit between them. Passing them to Begin keeps the description
// attached to what it describes:
//
//	ctx, op := w.o11y.Begin(ctx, observability.WithValues(map[string]any{
//		requestIDKey: req.ID,
//		statusKey:    string(req.Status),
//	}))
//	defer op.End()
//
// Options run in the order given, against the Operation and not the span
// directly, so a value seeded here lands on exactly the pillars the equivalent
// Operation method would have put it on — and a RecordingObserver sees it as an
// ordinary observation rather than as a special case.
//
// BeginCustom takes trace.SpanStartOption instead: a Go function may have only
// one variadic parameter, and for an explicitly named span the span options are
// the ones worth having. Seed such an operation by calling the Operation methods
// directly.
type BeginOption func(Operation)

// WithValues records values to both the span and the logger, as SetValues does.
//
// A nil or empty map is a no-op, so a caller may pass a map it built
// conditionally without guarding the call.
func WithValues(values map[string]any) BeginOption {
	return func(op Operation) {
		if len(values) == 0 {
			return
		}

		op.SetValues(values)
	}
}

// WithValue records one value to both the span and the logger, as Set does.
func WithValue(key string, value any) BeginOption {
	return func(op Operation) {
		op.Set(key, value)
	}
}

// WithSpanValue records one value to the span only, as SpanOnly does. Use it for
// values worth keeping on a trace but too noisy to repeat on every log line the
// operation emits.
func WithSpanValue(key string, value any) BeginOption {
	return func(op Operation) {
		op.SpanOnly(key, value)
	}
}

// WithLogValue records one value to the logger only, as LogOnly does.
func WithLogValue(key string, value any) BeginOption {
	return func(op Operation) {
		op.LogOnly(key, value)
	}
}

// applyBeginOptions runs opts against op, skipping nil entries so a caller may
// pass a conditionally built option without guarding it.
//
// It is shared by every Observer implementation, so a seeded value reaches the
// production observer and the recording one by the same path.
func applyBeginOptions(op Operation, opts []BeginOption) Operation {
	for _, opt := range opts {
		if opt != nil {
			opt(op)
		}
	}

	return op
}
