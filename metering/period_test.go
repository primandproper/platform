package metering

import (
	"context"
	"testing"
	"time"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestPeriod_Valid(T *testing.T) {
	T.Parallel()

	for _, p := range []Period{PeriodDay, PeriodMonth, PeriodBillingPeriod} {
		test.True(T, p.Valid(), test.Sprintf("period %q", p))
	}

	test.False(T, Period("").Valid())
	test.False(T, Period("fortnight").Valid())
}

func TestCalendarPeriodResolver(T *testing.T) {
	T.Parallel()

	resolver := NewCalendarPeriodResolver(nil)

	T.Run("resolves a UTC day", func(t *testing.T) {
		t.Parallel()

		bounds, err := resolver.Resolve(t.Context(), testSubject, PeriodDay, baseTime)
		must.NoError(t, err)

		test.EqOp(t, dayBounds.Start, bounds.Start)
		test.EqOp(t, dayBounds.End, bounds.End)
	})

	T.Run("resolves a UTC month", func(t *testing.T) {
		t.Parallel()

		bounds, err := resolver.Resolve(t.Context(), testSubject, PeriodMonth, baseTime)
		must.NoError(t, err)

		test.EqOp(t, monthBounds.Start, bounds.Start)
		test.EqOp(t, monthBounds.End, bounds.End)
	})

	T.Run("normalizes a non-UTC instant into the UTC window", func(t *testing.T) {
		t.Parallel()

		// 23:30 on the 15th in a zone eight hours behind UTC is 07:30 on the 16th
		// in UTC, and lands in the 16th's window. UTC rather than a configurable
		// zone is what keeps a daily quota exactly 24 hours long twice a year;
		// see NewCalendarPeriodResolver.
		zone := time.FixedZone("UTC-8", -8*60*60)
		local := time.Date(2026, time.August, 15, 23, 30, 0, 0, zone)

		bounds, err := resolver.Resolve(t.Context(), testSubject, PeriodDay, local)
		must.NoError(t, err)

		test.EqOp(t, time.Date(2026, time.August, 16, 0, 0, 0, 0, time.UTC), bounds.Start)
	})

	T.Run("wraps a year boundary", func(t *testing.T) {
		t.Parallel()

		bounds, err := resolver.Resolve(t.Context(), testSubject,
			PeriodMonth, time.Date(2026, time.December, 31, 23, 59, 59, 0, time.UTC))
		must.NoError(t, err)

		test.EqOp(t, time.Date(2026, time.December, 1, 0, 0, 0, 0, time.UTC), bounds.Start)
		test.EqOp(t, time.Date(2027, time.January, 1, 0, 0, 0, 0, time.UTC), bounds.End)
	})

	T.Run("refuses the billing period with no resolver", func(t *testing.T) {
		t.Parallel()

		// Refused rather than guessed. A calendar month assumed in place of a
		// subscription cycle bills the right total against the wrong invoice, and
		// nothing about the failure would be visible for a month.
		_, err := resolver.Resolve(t.Context(), testSubject, PeriodBillingPeriod, baseTime)

		test.ErrorIs(t, err, ErrNoBillingPeriodResolver)
	})

	T.Run("refuses an unknown period", func(t *testing.T) {
		t.Parallel()

		_, err := resolver.Resolve(t.Context(), testSubject, "fortnight", baseTime)

		test.ErrorIs(t, err, ErrUnknownPeriod)
	})
}

func TestCalendarPeriodResolver_Delegation(T *testing.T) {
	T.Parallel()

	anniversary := Bounds{
		Start: time.Date(2026, time.August, 9, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2026, time.September, 9, 0, 0, 0, 0, time.UTC),
	}

	T.Run("delegates the billing period", func(t *testing.T) {
		t.Parallel()

		var sawSubject string

		resolver := NewCalendarPeriodResolver(PeriodResolverFunc(
			func(_ context.Context, subject string, _ Period, _ time.Time) (Bounds, error) {
				sawSubject = subject

				return anniversary, nil
			}))

		bounds, err := resolver.Resolve(t.Context(), testSubject, PeriodBillingPeriod, baseTime)
		must.NoError(t, err)

		test.EqOp(t, testSubject, sawSubject)
		test.EqOp(t, anniversary.Start, bounds.Start)
		test.EqOp(t, anniversary.End, bounds.End)
	})

	T.Run("normalizes the delegate's answer to UTC", func(t *testing.T) {
		t.Parallel()

		zone := time.FixedZone("UTC+2", 2*60*60)

		resolver := NewCalendarPeriodResolver(PeriodResolverFunc(
			func(context.Context, string, Period, time.Time) (Bounds, error) {
				return Bounds{
					Start: anniversary.Start.In(zone),
					End:   anniversary.End.In(zone),
				}, nil
			}))

		bounds, err := resolver.Resolve(t.Context(), testSubject, PeriodBillingPeriod, baseTime)
		must.NoError(t, err)

		// The bounds become a primary key component, so their location has to be
		// pinned or the same window would key two different rows.
		test.EqOp(t, time.UTC, bounds.Start.Location())
		test.EqOp(t, anniversary.Start, bounds.Start)
	})

	T.Run("propagates the delegate's error", func(t *testing.T) {
		t.Parallel()

		resolver := NewCalendarPeriodResolver(PeriodResolverFunc(
			func(context.Context, string, Period, time.Time) (Bounds, error) {
				return Bounds{}, errArbitrary
			}))

		_, err := resolver.Resolve(t.Context(), testSubject, PeriodBillingPeriod, baseTime)

		test.ErrorIs(t, err, errArbitrary)
	})

	T.Run("refuses invalid bounds from the delegate", func(t *testing.T) {
		t.Parallel()

		// Vetted rather than trusted: a zero or inverted window would key every
		// period to the same row, which reads as one enormous month of usage.
		for _, bad := range []Bounds{
			{},
			{Start: anniversary.End, End: anniversary.Start},
			{Start: anniversary.Start, End: anniversary.Start},
		} {
			resolver := NewCalendarPeriodResolver(PeriodResolverFunc(
				func(context.Context, string, Period, time.Time) (Bounds, error) {
					return bad, nil
				}))

			_, err := resolver.Resolve(t.Context(), testSubject, PeriodBillingPeriod, baseTime)

			test.Error(t, err, test.Sprintf("bounds %v", bad))
		}
	})
}
