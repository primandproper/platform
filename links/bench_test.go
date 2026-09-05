package links

import (
	"testing"

	"github.com/shoenig/test/must"
)

// BenchmarkMinter measures the three operations against the in-memory store
// double, so the rows are this package's own cost rather than a network's.
// Redeem's premium over Inspect is the consuming write, which is what single
// use costs before a real store adds a lock or a transaction to it.
func BenchmarkMinter(b *testing.B) {
	m := newTestMinter(b)
	ctx := b.Context()

	b.Run("Mint", func(b *testing.B) {
		for b.Loop() {
			_, err := m.Mint(ctx, testAction, testSubject)
			must.NoError(b, err)
		}
	})

	b.Run("Inspect", func(b *testing.B) {
		link, err := m.Mint(ctx, testAction, testSubject)
		must.NoError(b, err)

		for b.Loop() {
			_, err = m.Inspect(ctx, link.Token)
			must.NoError(b, err)
		}
	})

	b.Run("Redeem", func(b *testing.B) {
		// A fresh link per iteration, minted outside the timer: redeeming the
		// same one twice would measure the refusal path instead.
		for b.Loop() {
			b.StopTimer()

			link, err := m.Mint(ctx, testAction, testSubject)
			must.NoError(b, err)

			b.StartTimer()

			_, err = m.Redeem(ctx, link.Token)
			must.NoError(b, err)
		}
	})

	b.Run("Redeem/spent", func(b *testing.B) {
		// The path a mail scanner's second visit takes, and the one that has to
		// stay cheap because a leaked URL is retried by everyone who has it.
		link, err := m.Mint(ctx, testAction, testSubject)
		must.NoError(b, err)

		_, err = m.Redeem(ctx, link.Token)
		must.NoError(b, err)

		for b.Loop() {
			_, _ = m.Redeem(ctx, link.Token)
		}
	})
}
