package audit

import (
	"strings"
	"testing"
	"time"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestCanonicalImage(T *testing.T) {
	T.Parallel()

	T.Run("is stable for the same entry", func(t *testing.T) {
		t.Parallel()

		entry := sampleEntry()

		test.Eq(t, canonicalImage(entry, []byte("{}"), nil), canonicalImage(entry, []byte("{}"), nil))
	})

	T.Run("separates fields unambiguously", func(t *testing.T) {
		t.Parallel()

		// Shifting a character across a field boundary. Under a delimited
		// framing these two entries would render identically, and a digest an
		// attacker can collide by choosing field values is not evidence of
		// anything.
		left := sampleEntry()
		left.ResourceType = "recipe"
		left.ResourceID = "1"

		right := sampleEntry()
		right.ResourceType = "recipe1"
		right.ResourceID = ""

		test.NotEq(t, canonicalImage(left, nil, nil), canonicalImage(right, nil, nil))
	})

	T.Run("covers every field that is stored", func(t *testing.T) {
		t.Parallel()

		base := canonicalImage(sampleEntry(), nil, nil)

		for name, mutate := range map[string]func(*Entry){
			"seq":          func(e *Entry) { e.Seq++ },
			"id":           func(e *Entry) { e.ID = "other" },
			"recordedAt":   func(e *Entry) { e.RecordedAt = e.RecordedAt.Add(time.Microsecond) },
			"scope":        func(e *Entry) { e.Scope = "other" },
			"resourceType": func(e *Entry) { e.ResourceType = "other" },
			"resourceID":   func(e *Entry) { e.ResourceID = "other" },
			"eventType":    func(e *Entry) { e.EventType = EventDeleted },
			"actorID":      func(e *Entry) { e.Actor.ID = "other" },
			"actorType":    func(e *Entry) { e.Actor.Type = ActorService },
			"actorIP":      func(e *Entry) { e.Actor.IP = "198.51.100.1" },
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				mutated := sampleEntry()
				mutate(mutated)

				test.NotEq(t, base, canonicalImage(mutated, nil, nil))
			})
		}
	})

	T.Run("covers the stored field blobs", func(t *testing.T) {
		t.Parallel()

		entry := sampleEntry()

		test.NotEq(t, canonicalImage(entry, []byte(`{"a":1}`), nil), canonicalImage(entry, []byte(`{"a":2}`), nil))
		test.NotEq(t, canonicalImage(entry, nil, []byte(`{"a":1}`)), canonicalImage(entry, nil, []byte(`{"a":2}`)))
	})

	T.Run("ignores the timestamp's location", func(t *testing.T) {
		t.Parallel()

		utc := sampleEntry()

		elsewhere := sampleEntry()
		elsewhere.RecordedAt = utc.RecordedAt.In(time.FixedZone("elsewhere", 5*60*60))

		// The same instant in two zones is the same entry. Postgres hands back
		// whatever the session is configured with, and a digest that varied with
		// that would fail on a replica configured differently.
		test.Eq(t, canonicalImage(utc, nil, nil), canonicalImage(elsewhere, nil, nil))
	})
}

func TestChainHash(T *testing.T) {
	T.Parallel()

	T.Run("depends on the predecessor", func(t *testing.T) {
		t.Parallel()

		image := canonicalImage(sampleEntry(), nil, nil)

		genesis, err := chainHash("", image)
		must.NoError(t, err)

		linked, err := chainHash(strings.Repeat("ab", 32), image)
		must.NoError(t, err)

		test.NotEq(t, genesis, linked)
		test.EqOp(t, 64, len(genesis))
	})

	T.Run("rejects a predecessor that is not a digest", func(t *testing.T) {
		t.Parallel()

		_, err := chainHash("not hex", nil)
		test.ErrorIs(t, err, ErrMalformedHash)
	})
}

func TestEncodeFields(T *testing.T) {
	T.Parallel()

	T.Run("collapses nil and empty to the same stored value", func(t *testing.T) {
		t.Parallel()

		fromNil, err := encodeFields[Change](nil)
		must.NoError(t, err)

		fromEmpty, err := encodeFields(map[string]Change{})
		must.NoError(t, err)

		// Two entries that say the same thing must hash the same, whichever way
		// a call site spelled "no changes".
		test.Nil(t, fromNil)
		test.Eq(t, fromNil, fromEmpty)
	})

	T.Run("sorts keys", func(t *testing.T) {
		t.Parallel()

		encoded, err := encodeFields(map[string]string{"b": "2", "a": "1"})
		must.NoError(t, err)

		test.EqOp(t, `{"a":"1","b":"2"}`, string(encoded))
	})
}

func TestHashValue(T *testing.T) {
	T.Parallel()

	T.Run("names its algorithm and hides its input", func(t *testing.T) {
		t.Parallel()

		hashed, err := hashValue("hunter2")
		must.NoError(t, err)

		test.True(t, strings.HasPrefix(hashed, "sha256:"))
		test.StrNotContains(t, hashed, "hunter2")
	})
}

// sampleEntry is a fully populated entry for the framing tests.
func sampleEntry() *Entry {
	return &Entry{
		RecordedAt:   time.Date(2026, time.July, 31, 12, 0, 0, 0, time.UTC),
		ID:           "entry_1",
		Seq:          3,
		Scope:        "acct_1",
		ResourceType: "recipe",
		ResourceID:   "recipe_1",
		EventType:    EventUpdated,
		Actor:        Actor{ID: "user_1", Type: ActorUser, IP: "203.0.113.7"},
	}
}
