package operations

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"strings"
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// exportRequest is the stand-in request type for these tests.
type exportRequest struct {
	SubjectID string `json:"subjectID"`
	Format    string `json:"format"`
}

// noopRun is a Run that does nothing, for the registrations whose Run is not
// what is being tested.
func noopRun[Req any](context.Context, Req, Reporter) (*Result, error) { return nil, nil }

func TestRegister(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		registry := NewRegistry()

		must.NoError(t, Register(registry, Definition[exportRequest]{
			Kind:       "dataprivacy.export",
			CountLabel: "records",
			Run:        noopRun[exportRequest],
		}))

		test.True(t, registry.Has("dataprivacy.export"))
		test.Eq(t, []string{"dataprivacy.export"}, registry.Kinds())
	})

	T.Run("a nil registry is an error, not a panic", func(t *testing.T) {
		t.Parallel()

		test.ErrorIs(t, Register(nil, Definition[exportRequest]{Kind: "a", Run: noopRun[exportRequest]}), ErrNilRegistry)
	})

	T.Run("rejects unusable definitions", func(t *testing.T) {
		t.Parallel()

		for name, kind := range map[string]string{
			"empty":            "",
			"uppercase":        "Export",
			"leading digit":    "1export",
			"trailing dot":     "export.",
			"double separator": "export..bundle",
			"spaces":           "an export of a subject's data",
			"too long":         strings.Repeat("a", MaxKindLength+1),
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				err := Register(NewRegistry(), Definition[exportRequest]{Kind: kind, Run: noopRun[exportRequest]})

				test.ErrorIs(t, err, ErrInvalidDefinition)
			})
		}
	})

	T.Run("rejects a definition with no Run", func(t *testing.T) {
		t.Parallel()

		test.ErrorIs(t,
			Register(NewRegistry(), Definition[exportRequest]{Kind: "export"}),
			ErrInvalidDefinition,
		)
	})

	// A silent overwrite would swap the Runner under operations that are already
	// queued, and the symptom — work done by the wrong code — arrives with
	// nothing to connect it to the second registration.
	T.Run("refuses a duplicate kind", func(t *testing.T) {
		t.Parallel()

		registry := NewRegistry()

		must.NoError(t, Register(registry, Definition[exportRequest]{Kind: "export", Run: noopRun[exportRequest]}))

		err := Register(registry, Definition[exportRequest]{Kind: "export", Run: noopRun[exportRequest]})

		test.ErrorIs(t, err, ErrDuplicateKind)
	})

	T.Run("Kinds is sorted", func(t *testing.T) {
		t.Parallel()

		registry := NewRegistry()

		for _, kind := range []string{"reindex", "export", "import"} {
			must.NoError(t, Register(registry, Definition[exportRequest]{Kind: kind, Run: noopRun[exportRequest]}))
		}

		test.Eq(t, []string{"export", "import", "reindex"}, registry.Kinds())
	})
}

func TestMustRegister(T *testing.T) {
	T.Parallel()

	T.Run("panics on an unusable definition", func(t *testing.T) {
		t.Parallel()

		defer func() { test.NotNil(t, recover()) }()

		MustRegister(NewRegistry(), Definition[exportRequest]{Kind: "", Run: noopRun[exportRequest]})
	})
}

func TestRegistry_lookup(T *testing.T) {
	T.Parallel()

	T.Run("an unknown kind names itself", func(t *testing.T) {
		t.Parallel()

		_, err := NewRegistry().lookup("nope")

		must.ErrorIs(t, err, ErrUnknownKind)
		test.StrContains(t, err.Error(), "nope")
	})

	T.Run("a nil registry is an error", func(t *testing.T) {
		t.Parallel()

		_, err := (*Registry)(nil).lookup("nope")

		test.ErrorIs(t, err, ErrNilRegistry)
		test.False(t, (*Registry)(nil).Has("nope"))
		test.Nil(t, (*Registry)(nil).Kinds())
	})
}

func TestRunner_encode(T *testing.T) {
	T.Parallel()

	registry := NewRegistry()
	must.NoError(T, Register(registry, Definition[exportRequest]{Kind: "export", Run: noopRun[exportRequest]}))

	bound, err := registry.lookup("export")
	must.NoError(T, err)

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		encoded, encodeErr := bound.encode(t.Context(), exportRequest{SubjectID: "s1", Format: "json"})

		must.NoError(t, encodeErr)
		test.StrContains(t, string(encoded), `"subjectID":"s1"`)
	})

	T.Run("nil encodes to nothing", func(t *testing.T) {
		t.Parallel()

		// "No request" and "an empty request" are the same thing, and both are
		// a legitimate way to start work that takes no parameters.
		encoded, encodeErr := bound.encode(t.Context(), nil)

		must.NoError(t, encodeErr)
		test.SliceEmpty(t, encoded)
	})

	// The check that earns its keep: a request of the wrong type very often
	// encodes to something the right type will happily decode, filling in
	// whichever fields happen to match. Without this the Runner receives a
	// plausible zero value, hours later, in a worker.
	T.Run("refuses the wrong request type", func(t *testing.T) {
		t.Parallel()

		type otherRequest struct {
			SubjectID string `json:"subjectID"`
		}

		_, encodeErr := bound.encode(t.Context(), otherRequest{SubjectID: "s1"})

		must.ErrorIs(t, encodeErr, ErrRequestTypeMismatch)
		test.StrContains(t, encodeErr.Error(), "export")
	})

	T.Run("refuses a request past the size limit", func(t *testing.T) {
		t.Parallel()

		_, encodeErr := bound.encode(t.Context(), exportRequest{SubjectID: strings.Repeat("s", MaxRequestBytes+1)})

		test.ErrorIs(t, encodeErr, ErrRequestTooLarge)
	})
}

func TestRunner_run(T *testing.T) {
	T.Parallel()

	T.Run("decodes and passes the request through", func(t *testing.T) {
		t.Parallel()

		var seen exportRequest

		registry := NewRegistry()
		must.NoError(t, Register(registry, Definition[exportRequest]{
			Kind: "export",
			Run: func(_ context.Context, req exportRequest, _ Reporter) (*Result, error) {
				seen = req

				return &Result{URI: "s3://bundle"}, nil
			},
		}))

		bound, err := registry.lookup("export")
		must.NoError(t, err)

		result, err := bound.run(t.Context(), json.RawMessage(`{"subjectID":"s1","format":"json"}`), nil)

		must.NoError(t, err)
		test.EqOp(t, "s3://bundle", result.URI)
		test.EqOp(t, "s1", seen.SubjectID)
		test.EqOp(t, "json", seen.Format)
	})

	T.Run("an absent request decodes to the zero value", func(t *testing.T) {
		t.Parallel()

		called := false

		registry := NewRegistry()
		must.NoError(t, Register(registry, Definition[exportRequest]{
			Kind: "reindex",
			Run: func(_ context.Context, req exportRequest, _ Reporter) (*Result, error) {
				called = true
				test.EqOp(t, "", req.SubjectID)

				return nil, nil
			},
		}))

		bound, err := registry.lookup("reindex")
		must.NoError(t, err)

		_, err = bound.run(t.Context(), nil, nil)

		must.NoError(t, err)
		test.True(t, called)
	})

	// A request the stored bytes cannot produce is not going to become one on a
	// second attempt, so it must not consume the whole attempt budget first.
	T.Run("an undecodable request fails unretryably", func(t *testing.T) {
		t.Parallel()

		registry := NewRegistry()
		must.NoError(t, Register(registry, Definition[exportRequest]{Kind: "export", Run: noopRun[exportRequest]}))

		bound, err := registry.lookup("export")
		must.NoError(t, err)

		_, err = bound.run(t.Context(), json.RawMessage(`not json`), nil)

		must.Error(t, err)
		test.True(t, IsUnretryable(err))
		test.EqOp(t, CodeInternal, codeOf(err))
	})
}

func TestRegistry_concurrentUse(T *testing.T) {
	T.Parallel()

	// The ordinary shape is register-then-read, but a registry is documented as
	// safe for concurrent use and the race detector is the only thing that can
	// say whether it is.
	registry := NewRegistry()

	must.NoError(T, Register(registry, Definition[exportRequest]{Kind: "export", Run: noopRun[exportRequest]}))

	done := make(chan struct{})

	go func() {
		defer close(done)

		for range 100 {
			_ = registry.Has("export")
			_ = registry.Kinds()
		}
	}()

	for i := range 100 {
		_ = Register(registry, Definition[exportRequest]{
			Kind: "kind" + string(rune('a'+i%26)),
			Run:  noopRun[exportRequest],
		})
	}

	<-done

	_, err := registry.lookup("export")
	test.NoError(T, err)

	test.False(T, stderrors.Is(err, ErrUnknownKind))
}
