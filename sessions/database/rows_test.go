package database

import (
	"testing"
	"time"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

var testEpoch = time.Date(2026, time.August, 8, 12, 0, 0, 0, time.UTC)

func TestBackend_row(T *testing.T) {
	T.Parallel()

	T.Run("carries the record's anchors and the payload it encoded", func(t *testing.T) {
		t.Parallel()

		backend, c := newTestBackend(t)

		r, err := backend.row(t.Context(), "id-1", testRecord(c, "u_1"), time.Hour)
		must.NoError(t, err)

		test.EqOp(t, "id-1", r.id)
		test.EqOp(t, c.Now().UTC(), r.createdAt)
		test.EqOp(t, c.Now().UTC(), r.lastSeenAt)
		test.EqOp(t, int64(1), r.version)
		test.SliceNotEmpty(t, r.data)
	})

	// The deadline is the backend's clock plus the TTL, because the interface
	// hands this layer a duration rather than an instant — and it is the same
	// clock Sweep binds, so a row written under a test clock is swept by one.
	T.Run("stamps the deadline from this backend's own clock", func(t *testing.T) {
		t.Parallel()

		backend, c := newTestBackend(t)

		r, err := backend.row(t.Context(), "id-1", testRecord(c, "u_1"), time.Hour)
		must.NoError(t, err)

		test.EqOp(t, c.Now().UTC().Add(time.Hour), r.expiresAt)
	})

	// Two of the three dialects store what the driver binds, so a time carrying
	// any other zone is stored with that zone's wall clock in it and every
	// later comparison is off by the offset — silently, and only for the
	// callers whose clock is not UTC.
	T.Run("converts every stamp to UTC", func(t *testing.T) {
		t.Parallel()

		backend, c := newTestBackend(t)

		elsewhere := time.FixedZone("somewhere", 5*60*60)
		record := testRecord(c, "u_1")
		record.CreatedAt = record.CreatedAt.In(elsewhere)
		record.LastSeenAt = record.LastSeenAt.In(elsewhere)

		r, err := backend.row(t.Context(), "id-1", record, time.Hour)
		must.NoError(t, err)

		test.EqOp(t, time.UTC, r.createdAt.Location())
		test.EqOp(t, time.UTC, r.lastSeenAt.Location())
		test.EqOp(t, time.UTC, r.expiresAt.Location())
	})

	// A session established without a payload stores NULL, which []byte(nil)
	// binds as on all three dialects, and reads back as the nil it went in as.
	T.Run("leaves an absent payload unencoded", func(t *testing.T) {
		t.Parallel()

		backend, c := newTestBackend(t)

		record := testRecord(c, "")
		record.Data = nil

		r, err := backend.row(t.Context(), "id-1", record, time.Hour)
		must.NoError(t, err)
		test.Nil(t, r.data)
	})
}

// TestRow_Params pins the projection of one encoded row onto the two generated
// parameter types, which is where the update's one structural guarantee lives:
// created_at has no field to carry it, so nothing can move the anchor of the
// absolute timeout by accident.
func TestRow_Params(T *testing.T) {
	T.Parallel()

	r := &row{
		createdAt:  testEpoch,
		lastSeenAt: testEpoch.Add(time.Minute),
		expiresAt:  testEpoch.Add(time.Hour),
		id:         "id-1",
		data:       []byte("payload"),
		version:    1,
	}

	T.Run("the create supplies every column", func(t *testing.T) {
		t.Parallel()

		create := r.create()

		test.EqOp(t, "id-1", create.ID)
		test.Eq(t, []byte("payload"), create.Data)
		test.EqOp(t, testEpoch, create.CreatedAt)
		test.EqOp(t, testEpoch.Add(time.Minute), create.LastSeenAt)
		test.EqOp(t, testEpoch.Add(time.Hour), create.ExpiresAt)
		test.EqOp(t, int64(1), create.Version)
	})

	T.Run("the update supplies the four it may assign", func(t *testing.T) {
		t.Parallel()

		create, update := r.create(), r.update()

		test.EqOp(t, create.ID, update.ID)
		test.Eq(t, create.Data, update.Data)
		test.EqOp(t, create.LastSeenAt, update.LastSeenAt)
		test.EqOp(t, create.ExpiresAt, update.ExpiresAt)
		test.EqOp(t, create.Version, update.Version)
	})
}
