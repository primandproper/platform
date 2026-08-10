package sessions

import (
	"testing"
	"time"

	"github.com/shoenig/test/must"
)

// Get runs on every authenticated request; the rest of the store's methods run
// at most once per request and usually far less often than that. The rows are
// arranged so that the per-request cost can be read off directly and the
// per-login costs sit next to it for scale.
//
// The backend is the in-process fake, so these are this package's own costs —
// policy evaluation, the touch decision, and the observability around them —
// rather than a Redis round trip. That is the point: the network cost is
// whatever the deployment's backend charges, and it is the same for every
// implementation, so the only number this package controls is this one.

func BenchmarkStore_Get(b *testing.B) {
	ctx := b.Context()

	// The clock is the fake one and nothing advances it here, so the touch
	// interval never elapses and Get never writes back. This is the ordinary
	// case: a session is touched once per interval, not once per request, and a
	// benchmark that touched every time would be measuring the exception.
	b.Run("withoutTouch", func(b *testing.B) {
		store, _, _ := newTestStore(b)
		b.Cleanup(func() { _ = store.Close() })

		session, err := store.New(ctx, &principal{})
		must.NoError(b, err)

		for b.Loop() {
			sessionSink, _ = store.Get(ctx, session.ID)
		}
	})

	// The interval that does elapse, so every Get writes the session back. This
	// is what a too-short touch interval actually costs: a read turns into a
	// read plus a write, on every request.
	b.Run("withTouch", func(b *testing.B) {
		store, _, clock := newTestStore(b, WithTouchInterval(time.Nanosecond))
		b.Cleanup(func() { _ = store.Close() })

		session, err := store.New(ctx, &principal{})
		must.NoError(b, err)

		for b.Loop() {
			clock.advance(time.Millisecond)
			sessionSink, _ = store.Get(ctx, session.ID)
		}
	})

	// A cookie naming a session that is gone: logged out, expired, or forged.
	// It has to stay cheap because an attacker chooses how often it happens.
	b.Run("missing", func(b *testing.B) {
		store, _, _ := newTestStore(b)
		b.Cleanup(func() { _ = store.Close() })

		for b.Loop() {
			sessionSink, _ = store.Get(ctx, "no-such-session")
		}
	})
}

// BenchmarkStore_Lifecycle prices the operations a session takes at its edges:
// one New per login, one Renew per privilege change, one Delete per logout.
func BenchmarkStore_Lifecycle(b *testing.B) {
	ctx := b.Context()

	b.Run("New", func(b *testing.B) {
		store, _, _ := newTestStore(b)
		b.Cleanup(func() { _ = store.Close() })

		for b.Loop() {
			sessionSink, _ = store.New(ctx, &principal{})
		}
	})

	b.Run("Save", func(b *testing.B) {
		store, _, _ := newTestStore(b)
		b.Cleanup(func() { _ = store.Close() })

		session, err := store.New(ctx, &principal{})
		must.NoError(b, err)

		for b.Loop() {
			_ = store.Save(ctx, session.ID, &principal{})
		}
	})

	// Renew rotates the identifier, which is the fixation defense and therefore
	// runs on every privilege escalation. A fresh session per iteration,
	// created outside the timer, because renewing a renewed ID measures the
	// miss path instead.
	b.Run("Renew", func(b *testing.B) {
		store, _, _ := newTestStore(b)
		b.Cleanup(func() { _ = store.Close() })

		for b.Loop() {
			b.StopTimer()

			session, err := store.New(ctx, &principal{})
			must.NoError(b, err)

			b.StartTimer()

			stringSink, _ = store.Renew(ctx, session.ID)
		}
	})

	b.Run("Delete", func(b *testing.B) {
		store, _, _ := newTestStore(b)
		b.Cleanup(func() { _ = store.Close() })

		for b.Loop() {
			b.StopTimer()

			session, err := store.New(ctx, &principal{})
			must.NoError(b, err)

			b.StartTimer()

			_ = store.Delete(ctx, session.ID)
		}
	})
}

// BenchmarkPolicy prices the pure computation every Get performs before it
// decides anything. These are the cheapest things in the package and are here
// so that the store rows above can be read as backend plus observability rather
// than as policy.
func BenchmarkPolicy(b *testing.B) {
	p := Policy{
		Idle:     30 * time.Minute,
		Absolute: 12 * time.Hour,
		Touch:    5 * time.Minute,
	}

	var (
		createdAt = time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
		lastSeen  = createdAt.Add(10 * time.Minute)
		now       = createdAt.Add(20 * time.Minute)
	)

	b.Run("Expiry", func(b *testing.B) {
		for b.Loop() {
			expirySink = p.Expiry(createdAt, lastSeen, now)
		}
	})

	b.Run("Deadline", func(b *testing.B) {
		for b.Loop() {
			timeSink = p.Deadline(createdAt, lastSeen)
		}
	})

	b.Run("ShouldTouch", func(b *testing.B) {
		for b.Loop() {
			boolSink = p.ShouldTouch(lastSeen, now)
		}
	})

	b.Run("TTL", func(b *testing.B) {
		for b.Loop() {
			durationSink = p.TTL(createdAt, now)
		}
	})
}

// BenchmarkNewID prices identifier generation, which every New and every Renew
// pays and which reads from the system's secure random source.
func BenchmarkNewID(b *testing.B) {
	ctx := b.Context()

	for b.Loop() {
		stringSink, _ = NewID(ctx)
	}
}

var (
	sessionSink  *Session[principal]
	stringSink   string
	timeSink     time.Time
	durationSink time.Duration
	expirySink   Expiry
	boolSink     bool
)
