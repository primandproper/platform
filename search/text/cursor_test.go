package textsearch

import (
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestCursor_IsZero(T *testing.T) {
	T.Parallel()

	T.Run("the empty cursor is zero", func(t *testing.T) {
		t.Parallel()

		test.True(t, Cursor("").IsZero())
	})

	T.Run("an issued cursor is not zero", func(t *testing.T) {
		t.Parallel()

		c, err := EncodeCursor("elasticsearch", 0)
		must.NoError(t, err)
		test.False(t, c.IsZero())
	})
}

func TestCursorRoundTrip(T *testing.T) {
	T.Parallel()

	T.Run("a cursor decodes to the position it was issued for", func(t *testing.T) {
		t.Parallel()

		for _, position := range []int{0, 1, 25, 9999} {
			c, err := EncodeCursor("elasticsearch", position)
			must.NoError(t, err)

			got, decodeErr := DecodeCursor("elasticsearch", c)
			must.NoError(t, decodeErr)
			test.EqOp(t, position, got)
		}
	})

	T.Run("the zero cursor decodes to the first page", func(t *testing.T) {
		t.Parallel()

		got, err := DecodeCursor("elasticsearch", "")
		must.NoError(t, err)
		test.EqOp(t, 0, got)
	})

	T.Run("a cursor is opaque rather than a bare number", func(t *testing.T) {
		t.Parallel()

		c, err := EncodeCursor("algolia", 3)
		must.NoError(t, err)
		test.NotEqOp(t, Cursor("3"), c)
	})
}

func TestDecodeCursor_Rejections(T *testing.T) {
	T.Parallel()

	T.Run("a cursor from another backend is refused", func(t *testing.T) {
		t.Parallel()

		// The two backends count in different units — a document offset versus a
		// page number — so accepting the wrong one silently skips or repeats
		// results rather than failing.
		c, err := EncodeCursor("elasticsearch", 50)
		must.NoError(t, err)

		got, decodeErr := DecodeCursor("algolia", c)
		test.ErrorIs(t, decodeErr, ErrInvalidCursor)
		test.EqOp(t, 0, got)
	})

	T.Run("a cursor that is not base64 is refused", func(t *testing.T) {
		t.Parallel()

		_, err := DecodeCursor("elasticsearch", "not a cursor!!")
		test.ErrorIs(t, err, ErrInvalidCursor)
	})

	T.Run("base64 that is not a cursor is refused", func(t *testing.T) {
		t.Parallel()

		_, err := DecodeCursor("elasticsearch", "bm90IGpzb24")
		test.ErrorIs(t, err, ErrInvalidCursor)
	})

	T.Run("a negative position is refused", func(t *testing.T) {
		t.Parallel()

		// Hand-built rather than round-tripped: EncodeCursor is not the only way
		// a caller can produce a string, and a negative offset reaching
		// Elasticsearch is a request error rather than a page.
		c, err := EncodeCursor("elasticsearch", -1)
		must.NoError(t, err)

		_, decodeErr := DecodeCursor("elasticsearch", c)
		test.ErrorIs(t, decodeErr, ErrInvalidCursor)
	})
}

func TestEffectiveLimit(T *testing.T) {
	T.Parallel()

	cases := map[string]struct {
		requested, max, expected int
	}{
		"unset takes the shared default":      {requested: 0, max: 200, expected: DefaultSearchLimit},
		"negative takes the shared default":   {requested: -5, max: 200, expected: DefaultSearchLimit},
		"a requested limit is honored":        {requested: 10, max: 200, expected: 10},
		"a limit past the ceiling is clamped": {requested: 5000, max: 200, expected: 200},
		"no ceiling leaves the request alone": {requested: 5000, max: 0, expected: 5000},
		"exactly the ceiling is honored":      {requested: 200, max: 200, expected: 200},
	}

	for name, tc := range cases {
		T.Run(name, func(t *testing.T) {
			t.Parallel()

			test.EqOp(t, tc.expected, EffectiveLimit(tc.requested, tc.max))
		})
	}
}

func TestSearchResults_Done(T *testing.T) {
	T.Parallel()

	T.Run("a nil result set is done", func(t *testing.T) {
		t.Parallel()

		var r *SearchResults[string]
		test.True(t, r.Done())
	})

	T.Run("no next cursor means done", func(t *testing.T) {
		t.Parallel()

		test.True(t, (&SearchResults[string]{}).Done())
	})

	T.Run("a next cursor means more pages", func(t *testing.T) {
		t.Parallel()

		c, err := EncodeCursor("algolia", 1)
		must.NoError(t, err)

		test.False(t, (&SearchResults[string]{NextCursor: c}).Done())
	})
}
