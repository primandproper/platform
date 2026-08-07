package cbormode

import (
	"bytes"
	"testing"
	"time"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

type example struct {
	At    time.Time      `json:"at"`
	Meta  map[string]any `json:"meta"`
	Name  string         `json:"name"`
	Tags  []string       `json:"tags"`
	Count int            `json:"count"`
}

func TestMarshal(T *testing.T) {
	T.Parallel()

	T.Run("round-trips a value", func(t *testing.T) {
		t.Parallel()

		original := example{
			Name:  "example",
			Count: 7,
			Tags:  []string{"a", "b"},
			Meta:  map[string]any{"tier": "standard"},
			At:    time.Date(2026, time.August, 3, 12, 30, 45, 123456789, time.UTC),
		}

		encoded, err := Marshal(original)
		must.NoError(t, err)
		must.SliceNotEmpty(t, encoded)

		var decoded example
		must.NoError(t, Unmarshal(encoded, &decoded))

		test.EqOp(t, original.Name, decoded.Name)
		test.EqOp(t, original.Count, decoded.Count)
		test.Eq(t, original.Tags, decoded.Tags)
		test.Eq(t, original.Meta, decoded.Meta)
		test.True(t, original.At.Equal(decoded.At))
	})

	T.Run("keeps nanosecond precision on time", func(t *testing.T) {
		t.Parallel()

		// The whole reason this package exists rather than calling
		// cbor.Marshal: the library default truncates to whole seconds.
		original := time.Date(2026, time.August, 3, 12, 30, 45, 123456789, time.UTC)

		encoded, err := Marshal(original)
		must.NoError(t, err)

		var decoded time.Time
		must.NoError(t, Unmarshal(encoded, &decoded))

		test.EqOp(t, original.Nanosecond(), decoded.Nanosecond())
		test.True(t, original.Equal(decoded))
	})

	T.Run("uses json tags when no cbor tag is present", func(t *testing.T) {
		t.Parallel()

		encoded, err := Marshal(struct {
			Name string `json:"name"`
		}{Name: "tagged"})
		must.NoError(t, err)

		var decoded map[string]any
		must.NoError(t, Unmarshal(encoded, &decoded))

		test.MapContainsKey(t, decoded, "name")
	})

	T.Run("decodes maps into map[string]any for untyped destinations", func(t *testing.T) {
		t.Parallel()

		encoded, err := Marshal(map[string]any{"nested": map[string]any{"k": "v"}})
		must.NoError(t, err)

		var decoded any
		must.NoError(t, Unmarshal(encoded, &decoded))

		// Not map[any]any, which is the library default and cannot be
		// re-marshaled as JSON.
		asMap, ok := decoded.(map[string]any)
		must.True(t, ok)
		test.MapContainsKey(t, asMap, "nested")
	})

	T.Run("decodes from a reader", func(t *testing.T) {
		t.Parallel()

		encoded, err := Marshal(example{Name: "streamed"})
		must.NoError(t, err)

		var decoded example
		must.NoError(t, NewDecoder(bytes.NewReader(encoded)).Decode(&decoded))
		test.EqOp(t, "streamed", decoded.Name)
	})

	T.Run("rejects malformed data", func(t *testing.T) {
		t.Parallel()

		var decoded example
		test.Error(t, Unmarshal([]byte{0xff, 0xff, 0xff}, &decoded))
	})
}
