package database

import (
	"context"
	"time"

	platformerrors "github.com/primandproper/platform-go/v13/errors"
	"github.com/primandproper/platform-go/v13/sessions"
	"github.com/primandproper/platform-go/v13/sessions/database/internal/sessionsdb"
)

// row is one record's worth of bound parameters, with the payload already
// encoded.
//
// It exists because two statements bind the same values under two generated
// parameter types: a create supplies every column, an update supplies the four
// it may assign. Encoding the record once and projecting it twice is what keeps
// a session written by Create and the same session written by Update from
// differing in anything but the columns the update deliberately leaves alone.
type row struct {
	createdAt  time.Time
	lastSeenAt time.Time
	expiresAt  time.Time
	id         string
	data       []byte
	version    int64
}

// row encodes a record into bound parameters.
//
// Times are converted to UTC here rather than at each statement. Two of the
// three dialects store what the driver binds, so a time carrying any other zone
// is stored with that zone's wall clock in it and every later comparison is off
// by the offset — silently, and only for the callers whose clock is not UTC.
func (b *Backend[T]) row(
	ctx context.Context,
	id string,
	record *sessions.Record[T],
	ttl time.Duration,
) (*row, error) {
	r := &row{
		createdAt:  record.CreatedAt.UTC(),
		lastSeenAt: record.LastSeenAt.UTC(),
		// Stamped from this backend's clock rather than from the store's, since
		// the interface passes a duration. It feeds the sweeper and nothing
		// else — whether a session is live is decided from the two anchors
		// above — so the two clocks disagreeing costs a row swept early or
		// late, never a session that reads as live when it is not.
		//
		// It is also why Sweep binds its own reading of this same clock rather
		// than asking the server for the time; see the sweep statement in
		// sessions/database/internal/queries.
		expiresAt: b.clock.Now().UTC().Add(ttl),
		id:        id,
		version:   int64(record.Version),
	}

	if record.Data == nil {
		return r, nil
	}

	data, err := b.codec.Marshal(ctx, record.Data)
	if err != nil {
		return nil, platformerrors.Wrap(err, "encoding session payload")
	}

	r.data = data

	return r, nil
}

// create renders the row as the create's arguments.
func (r *row) create() sessionsdb.CreateSessionParams {
	return sessionsdb.CreateSessionParams{
		ID:         r.id,
		Data:       r.data,
		CreatedAt:  r.createdAt,
		LastSeenAt: r.lastSeenAt,
		ExpiresAt:  r.expiresAt,
		Version:    r.version,
	}
}

// update renders the row as the overwrite's arguments.
//
// created_at is absent because the statement does not assign it: the anchor of
// the absolute timeout is not something an update can move, and it is left out
// of the SET list rather than passed and ignored — see the update statement in
// sessions/database/internal/queries.
func (r *row) update() sessionsdb.UpdateSessionParams {
	return sessionsdb.UpdateSessionParams{
		ID:         r.id,
		Data:       r.data,
		LastSeenAt: r.lastSeenAt,
		ExpiresAt:  r.expiresAt,
		Version:    r.version,
	}
}
