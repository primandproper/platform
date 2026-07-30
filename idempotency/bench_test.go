package idempotency

import (
	"context"
	"strconv"
	"testing"

	"github.com/shoenig/test/must"
)

// BenchmarkManager_Do separates the two paths that matter operationally.
//
// Replay is the path nearly every duplicate takes, and it should cost one store
// read with no coordination at all — if it ever shows lock acquisition, the
// pre-lock read has regressed. Execute pays for the lock, the claim, and the
// completion write, and is what a first-time request costs.
func BenchmarkManager_Do(b *testing.B) {
	work := func(context.Context) (*payload, error) { return &payload{Name: "v"}, nil }

	b.Run("Replay", func(b *testing.B) {
		m := newBenchManager(b)
		ctx := b.Context()

		_, err := m.Do(ctx, "bench", testFingerprint, work)
		must.NoError(b, err)

		for b.Loop() {
			_, _ = m.Do(ctx, "bench", testFingerprint, work)
		}
	})

	b.Run("Execute", func(b *testing.B) {
		m := newBenchManager(b)
		ctx := b.Context()

		var i int
		for b.Loop() {
			i++
			_, _ = m.Do(ctx, Key(strconv.Itoa(i)), testFingerprint, work)
		}
	})

	// The refusal path. It resolves from the pre-lock read alone, so it should
	// cost the same as a replay.
	b.Run("InFlight", func(b *testing.B) {
		m := newBenchManager(b)
		ctx := b.Context()

		must.NoError(b, m.store.Set(ctx, m.storeKey("bench"), inFlight(testFingerprint)))

		for b.Loop() {
			_, _ = m.Do(ctx, "bench", testFingerprint, work)
		}
	})
}

func BenchmarkValidateKey(b *testing.B) {
	const key = "d3f1a0c4-5b6e-4a2f-9c8d-1e2f3a4b5c6d"

	for b.Loop() {
		_ = ValidateKey(key, DefaultMaxKeyLength)
	}
}

func newBenchManager(b *testing.B) *Manager[payload] {
	b.Helper()

	m, err := NewManager(newStore(b), newLocker(b))
	must.NoError(b, err)

	return m
}
