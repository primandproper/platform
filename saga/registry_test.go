package saga

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func noopStep(name string) Step[testState] {
	return Step[testState]{
		Name: name,
		Do:   func(context.Context, *testState) error { return nil },
	}
}

func TestRegister(T *testing.T) {
	T.Parallel()

	T.Run("registers a definition", func(t *testing.T) {
		t.Parallel()

		registry := NewRegistry()

		must.NoError(t, Register(registry, Definition[testState]{
			Name:  "orders",
			Steps: []Step[testState]{noopStep("one"), noopStep("two")},
		}))

		test.Eq(t, []string{"orders"}, registry.Names())

		names, ok := registry.StepNames("orders")
		must.True(t, ok)
		test.Eq(t, []string{"one", "two"}, names)
	})

	T.Run("rejects a nil registry", func(t *testing.T) {
		t.Parallel()

		test.ErrorIs(t, Register[testState](nil, Definition[testState]{}), ErrNilRegistry)
	})

	T.Run("rejects a duplicate name", func(t *testing.T) {
		t.Parallel()

		registry := NewRegistry()
		def := Definition[testState]{Name: "orders", Steps: []Step[testState]{noopStep("one")}}

		must.NoError(t, Register(registry, def))
		test.ErrorIs(t, Register(registry, def), ErrDuplicateDefinition)
	})

	T.Run("rejects invalid definitions", func(t *testing.T) {
		t.Parallel()

		for name, def := range map[string]Definition[testState]{
			"no name": {Steps: []Step[testState]{noopStep("one")}},
			"no steps": {
				Name: "orders",
			},
			"unnamed step": {
				Name:  "orders",
				Steps: []Step[testState]{{Do: func(context.Context, *testState) error { return nil }}},
			},
			"step name with a space": {
				Name:  "orders",
				Steps: []Step[testState]{noopStep("charge card")},
			},
			"step name with a colon": {
				Name:  "orders",
				Steps: []Step[testState]{noopStep("charge:card")},
			},
			"step name too long": {
				Name:  "orders",
				Steps: []Step[testState]{noopStep(strings.Repeat("x", maxStepNameLength+1))},
			},
			"no do": {
				Name:  "orders",
				Steps: []Step[testState]{{Name: "one"}},
			},
			"negative delay": {
				Name: "orders",
				Steps: []Step[testState]{{
					Name:  "one",
					Do:    func(context.Context, *testState) error { return nil },
					Delay: -time.Second,
				}},
			},
			"repeated step name": {
				Name:  "orders",
				Steps: []Step[testState]{noopStep("one"), noopStep("one")},
			},
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				test.ErrorIs(t, Register(NewRegistry(), def), ErrInvalidDefinition)
			})
		}
	})

	T.Run("copies the caller's step slice", func(t *testing.T) {
		t.Parallel()

		registry := NewRegistry()
		steps := []Step[testState]{noopStep("one"), noopStep("two")}

		must.NoError(t, Register(registry, Definition[testState]{Name: "orders", Steps: steps}))

		// Reordering the caller's slice must not reorder what was registered:
		// the registered list is what every drift check compares against.
		steps[0], steps[1] = steps[1], steps[0]

		names, ok := registry.StepNames("orders")
		must.True(t, ok)
		test.Eq(t, []string{"one", "two"}, names)
	})

	T.Run("StepNames clones", func(t *testing.T) {
		t.Parallel()

		registry := NewRegistry()
		must.NoError(t, Register(registry, Definition[testState]{
			Name:  "orders",
			Steps: []Step[testState]{noopStep("one")},
		}))

		names, ok := registry.StepNames("orders")
		must.True(t, ok)

		names[0] = "tampered"

		again, ok := registry.StepNames("orders")
		must.True(t, ok)
		test.Eq(t, []string{"one"}, again)
	})

	T.Run("StepNames reports an unknown definition", func(t *testing.T) {
		t.Parallel()

		names, ok := NewRegistry().StepNames("nope")
		test.False(t, ok)
		test.Nil(t, names)
	})

	T.Run("Names is sorted", func(t *testing.T) {
		t.Parallel()

		registry := NewRegistry()
		for _, name := range []string{"zeta", "alpha", "mu"} {
			must.NoError(t, Register(registry, Definition[testState]{
				Name:  name,
				Steps: []Step[testState]{noopStep("one")},
			}))
		}

		test.Eq(t, []string{"alpha", "mu", "zeta"}, registry.Names())
	})
}

func TestDefinition_apply(T *testing.T) {
	T.Parallel()

	T.Run("round-trips state through a step", func(t *testing.T) {
		t.Parallel()

		registry := NewRegistry()
		must.NoError(t, Register(registry, Definition[testState]{
			Name: "orders",
			Steps: []Step[testState]{{
				Name: "bump",
				Do: func(_ context.Context, s *testState) error {
					s.Amount += 5

					return nil
				},
				Undo: func(_ context.Context, s *testState) error {
					s.Amount -= 5

					return nil
				},
			}},
		}))

		def, ok := registry.lookup("orders")
		must.True(t, ok)
		test.EqOp(t, 1, def.steps())
		test.True(t, def.compensates[0])

		out, err := def.do(t.Context(), 0, json.RawMessage(`{"amount":10}`))
		must.NoError(t, err)

		var got testState
		must.NoError(t, json.Unmarshal(out, &got))
		test.EqOp(t, 15, got.Amount)

		back, err := def.undo(t.Context(), 0, out)
		must.NoError(t, err)

		must.NoError(t, json.Unmarshal(back, &got))
		test.EqOp(t, 10, got.Amount)
	})

	T.Run("an absent state decodes to the zero value", func(t *testing.T) {
		t.Parallel()

		registry := NewRegistry()
		must.NoError(t, Register(registry, Definition[testState]{
			Name: "orders",
			Steps: []Step[testState]{{
				Name: "check",
				Do: func(_ context.Context, s *testState) error {
					test.EqOp(t, 0, s.Amount)

					return nil
				},
			}},
		}))

		def, _ := registry.lookup("orders")

		_, err := def.do(t.Context(), 0, nil)
		test.NoError(t, err)
	})

	T.Run("reports a state that will not decode", func(t *testing.T) {
		t.Parallel()

		registry := NewRegistry()
		must.NoError(t, Register(registry, Definition[testState]{
			Name:  "orders",
			Steps: []Step[testState]{noopStep("one")},
		}))

		def, _ := registry.lookup("orders")

		_, err := def.do(t.Context(), 0, json.RawMessage(`not json`))
		test.Error(t, err)
	})

	T.Run("reports a state that will not encode", func(t *testing.T) {
		t.Parallel()

		type unencodable struct {
			Fn func() `json:"fn"`
		}

		registry := NewRegistry()
		must.NoError(t, Register(registry, Definition[unencodable]{
			Name: "orders",
			Steps: []Step[unencodable]{{
				Name: "one",
				Do: func(_ context.Context, s *unencodable) error {
					s.Fn = func() {}

					return nil
				},
			}},
		}))

		def, _ := registry.lookup("orders")

		_, err := def.do(t.Context(), 0, json.RawMessage(`{}`))
		test.Error(t, err)
	})

	T.Run("returns the step's own error untouched", func(t *testing.T) {
		t.Parallel()

		sentinel := errors.New("the card was declined")

		registry := NewRegistry()
		must.NoError(t, Register(registry, Definition[testState]{
			Name: "orders",
			Steps: []Step[testState]{{
				Name: "charge",
				Do:   func(context.Context, *testState) error { return sentinel },
			}},
		}))

		def, _ := registry.lookup("orders")

		_, err := def.do(t.Context(), 0, json.RawMessage(`{}`))
		test.ErrorIs(t, err, sentinel)
	})
}

func TestValidStepName(T *testing.T) {
	T.Parallel()

	for name, tc := range map[string]struct {
		in   string
		want bool
	}{
		"plain":       {"charge_card", true},
		"hyphenated":  {"reserve-inventory", true},
		"mixed case":  {"NotifyPartner", true},
		"empty":       {"", false},
		"space":       {"charge card", false},
		"colon":       {"charge:card", false},
		"tab":         {"charge\tcard", false},
		"non-ascii":   {"chargé", false},
		"at the max":  {strings.Repeat("x", maxStepNameLength), true},
		"overit":      {strings.Repeat("x", maxStepNameLength+1), false},
		"del control": {"charge\x7f", false},
	} {
		T.Run(name, func(t *testing.T) {
			t.Parallel()

			test.EqOp(t, tc.want, validStepName(tc.in))
		})
	}
}
