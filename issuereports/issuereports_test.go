package issuereports

import (
	"testing"
	"time"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestParseStatus(T *testing.T) {
	T.Parallel()

	T.Run("normalizes what a client sends", func(t *testing.T) {
		t.Parallel()

		// The string reaching this was typed into a form or a query string.
		// "Open" and "open" are one status, and a store that accepted both would
		// hold reports in two queues neither of which is the whole queue.
		for _, spelling := range []string{"open", "Open", "OPEN", " open ", "\tOpen\n"} {
			status, ok := ParseStatus(spelling)
			test.True(t, ok, test.Sprintf("%q", spelling))
			test.EqOp(t, StatusOpen, status, test.Sprintf("%q", spelling))
		}

		status, ok := ParseStatus("Declined")
		test.True(t, ok)
		test.EqOp(t, StatusDeclined, status)
	})

	T.Run("refuses a status no queue lists", func(t *testing.T) {
		t.Parallel()

		for _, spelling := range []string{"", "closed", "triaged", "in progress"} {
			_, ok := ParseStatus(spelling)
			test.False(t, ok, test.Sprintf("%q", spelling))
		}
	})
}

func TestStatus_Valid(t *testing.T) {
	t.Parallel()

	for _, status := range Statuses {
		test.True(t, status.Valid(), test.Sprintf("%q", status))
	}

	test.False(t, Status("").Valid())
	test.False(t, Status("OPEN").Valid())
	test.False(t, Status("closed").Valid())
}

func TestStatus_Terminal(t *testing.T) {
	t.Parallel()

	test.False(t, StatusOpen.Terminal())
	test.False(t, StatusAcknowledged.Terminal())
	test.True(t, StatusResolved.Terminal())
	test.True(t, StatusDeclined.Terminal())
}

func TestStatus_String(t *testing.T) {
	t.Parallel()

	test.EqOp(t, "open", StatusOpen.String())
	test.EqOp(t, "acknowledged", StatusAcknowledged.String())
}

func TestStatus_CanTransitionTo(T *testing.T) {
	T.Parallel()

	T.Run("the lifecycle, spelled out", func(t *testing.T) {
		t.Parallel()

		// Written as the whole matrix rather than as the allowed moves, because
		// what a reader needs from this is the shape, and a list of permitted
		// pairs says nothing about the pairs that are missing from it.
		allowed := map[Status]map[Status]bool{
			StatusOpen: {
				StatusAcknowledged: true, StatusResolved: true, StatusDeclined: true,
			},
			StatusAcknowledged: {
				StatusResolved: true, StatusDeclined: true,
			},
			StatusResolved: {StatusOpen: true},
			StatusDeclined: {StatusOpen: true},
		}

		for _, from := range Statuses {
			for _, to := range Statuses {
				test.EqOp(t, allowed[from][to], from.CanTransitionTo(to),
					test.Sprintf("%q to %q", from, to))
			}
		}
	})

	T.Run("nothing transitions to itself", func(t *testing.T) {
		t.Parallel()

		// A second resolve is not a no-op: it would move closed_at forward and
		// overwrite the note, and the caller doing it is acting on a view of the
		// row that is one write out of date.
		for _, status := range Statuses {
			test.False(t, status.CanTransitionTo(status), test.Sprintf("%q", status))
		}
	})

	T.Run("an unknown status can move nowhere and nothing can move to it", func(t *testing.T) {
		t.Parallel()

		unknown := Status("triaged")

		test.False(t, unknown.CanTransitionTo(StatusOpen))

		for _, status := range Statuses {
			test.False(t, status.CanTransitionTo(unknown), test.Sprintf("%q", status))
		}
	})

	T.Run("every status is reachable and every one leads somewhere", func(t *testing.T) {
		t.Parallel()

		// A status nothing leads to is one no report can ever be in; one that
		// leads nowhere is a hole a report falls into. Neither is visible from a
		// per-transition assertion.
		reachable := map[Status]bool{StatusOpen: true}

		for _, from := range Statuses {
			leadsSomewhere := false

			for _, to := range Statuses {
				if from.CanTransitionTo(to) {
					reachable[to] = true
					leadsSomewhere = true
				}
			}

			test.True(t, leadsSomewhere, test.Sprintf("%q leads nowhere", from))
		}

		for _, status := range Statuses {
			test.True(t, reachable[status], test.Sprintf("%q is unreachable", status))
		}
	})
}

func TestStatuses(t *testing.T) {
	t.Parallel()

	// The exported list and the lifecycle table are two spellings of the same
	// set, and a console rendering one queue per entry of the first would
	// silently drop a status that only appeared in the second.
	must.SliceLen(t, len(transitions), Statuses)

	for _, status := range Statuses {
		_, ok := transitions[status]
		test.True(t, ok, test.Sprintf("%q", status))
	}
}

func TestReport_Closed(t *testing.T) {
	t.Parallel()

	at := time.Now().UTC()

	test.False(t, (*Report)(nil).Closed())
	test.False(t, (&Report{Status: StatusOpen}).Closed())
	test.False(t, (&Report{Status: StatusAcknowledged, ClosedAt: &at}).Closed())
	test.True(t, (&Report{Status: StatusResolved}).Closed())
	test.True(t, (&Report{Status: StatusDeclined}).Closed())
}

func TestReporterAttributeKey(t *testing.T) {
	t.Parallel()

	// Exported so a consumer's attributes agree with this package's rather than
	// merely resembling them; if the two ever diverge, a dashboard grouping on
	// one silently loses half its series.
	test.EqOp(t, reporterKey, ReporterAttributeKey)
}
