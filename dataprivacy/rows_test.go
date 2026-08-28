package dataprivacy

import (
	"testing"
	"time"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestInstant(T *testing.T) {
	T.Parallel()

	T.Run("maps the zero time to NULL", func(t *testing.T) {
		t.Parallel()

		// Bound as a value it reads back as year 1, which every horizon
		// comparison in the sweeps would treat as long overdue — so an erasure
		// with no confirmation window would be lapsed by the first sweep that
		// saw it.
		test.Nil(t, instant(time.Time{}))
	})

	T.Run("normalizes to UTC", func(t *testing.T) {
		t.Parallel()

		local := baseTime.In(time.FixedZone("UTC+2", 2*60*60))

		got := instant(local)
		must.NotNil(t, got)

		// SQLite stores these as text and compares them as text, and the
		// generated bindings render a bound time at whatever zone it carries —
		// so a value that is not UTC puts its own wall clock in the leading
		// characters the comparison reads.
		test.EqOp(t, time.UTC, got.Location())
		test.True(t, got.Equal(baseTime))
	})
}

func TestStringValue(T *testing.T) {
	T.Parallel()

	T.Run("reads a NULL column as the empty string", func(t *testing.T) {
		t.Parallel()

		// The column is nullable because rows written before there was anything
		// to say carry NULL. Nothing this package writes adds another one — the
		// empty string is bound as the empty string — so the two spellings mean
		// the same thing on the way out while the way in has only one.
		test.EqOp(t, "", stringValue(nil))
	})

	T.Run("round trips what was written", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, "boom", stringValue(new("boom")))
		test.EqOp(t, "", stringValue(new("")))
	})
}

func TestUTCPtr(T *testing.T) {
	T.Parallel()

	T.Run("preserves absence", func(t *testing.T) {
		t.Parallel()

		test.Nil(t, utcPtr(nil))
	})

	T.Run("normalizes what is there", func(t *testing.T) {
		t.Parallel()

		local := baseTime.In(time.FixedZone("UTC-5", -5*60*60))

		got := utcPtr(&local)
		must.NotNil(t, got)
		test.EqOp(t, time.UTC, got.Location())
		test.True(t, got.Equal(baseTime))
	})
}

func TestUTCValue(T *testing.T) {
	T.Parallel()

	T.Run("an absent timestamp is the zero time", func(t *testing.T) {
		t.Parallel()

		// Request.ExpiresAt is a value, and its zero has always meant "this
		// request was never held for confirmation and has no artifact to
		// expire".
		test.True(t, utcValue(nil).IsZero())
	})

	T.Run("a present one comes back in UTC", func(t *testing.T) {
		t.Parallel()

		local := baseTime.In(time.FixedZone("UTC+9", 9*60*60))

		test.EqOp(t, time.UTC, utcValue(&local).Location())
	})
}

func TestEncodeMap(T *testing.T) {
	T.Parallel()

	T.Run("an empty map is stored as nothing", func(t *testing.T) {
		t.Parallel()

		// Nil and empty collapse deliberately: they say the same thing, and
		// storing two renderings would make a round trip depend on which call
		// site wrote the row.
		for _, m := range []map[string]string{nil, {}} {
			encoded, err := encodeMap(m)
			must.NoError(t, err)
			test.Nil(t, encoded)
		}
	})

	T.Run("round trips a populated map", func(t *testing.T) {
		t.Parallel()

		encoded, err := encodeMap(map[string]string{"billing": "timed out"})
		must.NoError(t, err)

		decoded, err := decodeMap(encoded)
		must.NoError(t, err)
		test.Eq(t, map[string]string{"billing": "timed out"}, decoded)
	})

	T.Run("an absent column decodes to no map and no error", func(t *testing.T) {
		t.Parallel()

		decoded, err := decodeMap(nil)
		must.NoError(t, err)
		test.Nil(t, decoded)
	})

	T.Run("a column holding something else is an error", func(t *testing.T) {
		t.Parallel()

		_, err := decodeMap([]byte(`["not","a","map"]`))
		test.Error(t, err)
	})
}
