package timers

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

const testTable = "scheduled_timers"

// placeholderPattern finds every bind marker in a rendered statement.
var placeholderPattern = regexp.MustCompile(`\$(\d+)`)

// assertContiguousPlaceholders proves a statement binds $1..$n with nothing
// skipped. Postgres numbers placeholders globally, and these statements build
// theirs from several fragments — a gap means one fragment was renumbered and
// another was not, which shows up as a runtime bind error rather than a
// compile-time one.
func assertContiguousPlaceholders(t *testing.T, query string, want int) {
	t.Helper()

	seen := map[string]struct{}{}
	for _, match := range placeholderPattern.FindAllString(query, -1) {
		seen[match] = struct{}{}
	}

	test.MapLen(t, want, seen, test.Sprintf("query: %s", query))

	for i := 1; i <= want; i++ {
		marker := "$" + strconv.Itoa(i)
		_, ok := seen[marker]
		test.True(t, ok, test.Sprintf("missing %s in: %s", marker, query))
	}
}

func TestTableFor(T *testing.T) {
	T.Parallel()

	T.Run("an empty namespace renders the component's own name", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, "scheduled_timers", tableFor(""))
	})

	T.Run("a namespace is separated by the renderer", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, "ddb_scheduled_timers", tableFor("ddb"))
	})
}

func TestBuildSchedule(T *testing.T) {
	T.Parallel()

	instant := time.Date(2026, time.August, 21, 9, 0, 0, 0, time.UTC)

	T.Run("binds four parameters per row", func(t *testing.T) {
		t.Parallel()

		rows := []encodedTimer{
			{key: "a", runAt: instant},
			{key: "b", runAt: instant.Add(time.Hour), payload: []byte("note")},
		}

		query, args := buildSchedule(testTable, "trials", rows)

		must.SliceLen(t, 8, args)
		assertContiguousPlaceholders(t, query, 8)
		test.EqOp(t, "trials", args[0])
		test.EqOp(t, "a", args[1])
		test.EqOp(t, "b", args[5])
	})

	// The one rule that distinguishes this from a work queue's enqueue. A merge
	// that only ever moved a timer earlier could not express the case the whole
	// feature exists for: a deadline that moved out.
	// created_at is left to the column's own default and last_updated_at is left
	// unwritten, so a timer nobody has moved has no last mutation. Rescheduling
	// is an update, and it stamps one rather than rewriting the creation time —
	// which is what a scheduled_at reset used to do.
	T.Run("the insert defaults created_at and the reschedule stamps the mutation", func(t *testing.T) {
		t.Parallel()

		query, _ := buildSchedule(testTable, "trials", []encodedTimer{{key: "a", runAt: instant}})

		inserted, conflict, found := strings.Cut(query, " VALUES ")
		must.True(t, found)
		test.StrNotContains(t, inserted, "created_at")
		test.StrNotContains(t, inserted, "last_updated_at")
		test.StrContains(t, conflict, "last_updated_at = now()")
		test.StrNotContains(t, conflict, "created_at = ")
	})

	T.Run("rescheduling takes the new instant outright, in either direction", func(t *testing.T) {
		t.Parallel()

		query, _ := buildSchedule(testTable, "trials", []encodedTimer{{key: "a", runAt: instant}})

		test.True(t, strings.Contains(query, "run_at = excluded.run_at"))
		test.False(t, strings.Contains(query, "LEAST"))
		test.False(t, strings.Contains(query, "GREATEST"))
	})

	// A reschedule is a new schedule, not a retry of the old one, so nothing the
	// old one accumulated may survive it.
	T.Run("rescheduling resets the attempt count, the error, and the firing", func(t *testing.T) {
		t.Parallel()

		query, _ := buildSchedule(testTable, "trials", []encodedTimer{{key: "a", runAt: instant}})

		test.True(t, strings.Contains(query, "attempts = 0"))
		test.True(t, strings.Contains(query, "last_error = NULL"))
		test.True(t, strings.Contains(query, "fired_at = NULL"))
	})

	// A move frees the row, because the lease belongs to a schedule that no
	// longer exists. A reschedule to the same instant does not, because that is
	// what an at-least-once redelivery looks like and freeing the row there
	// would hand a live firing to a second worker.
	T.Run("rescheduling revokes the lease only when the instant moved", func(t *testing.T) {
		t.Parallel()

		query, _ := buildSchedule(testTable, "trials", []encodedTimer{{key: "a", runAt: instant}})

		test.True(t, strings.Contains(query,
			"lease_until = CASE WHEN s.run_at IS DISTINCT FROM excluded.run_at "+
				"THEN TIMESTAMPTZ 'epoch' ELSE s.lease_until END"))
	})
}

func TestBuildClaim(T *testing.T) {
	T.Parallel()

	T.Run("binds the set, the ceiling, the limit, and the lease", func(t *testing.T) {
		t.Parallel()

		assertContiguousPlaceholders(t, buildClaim(testTable), 4)
	})

	// Postgres applies a LIMIT above the lock, so a row a competitor holds is
	// skipped and replaced rather than counted against the batch. Pushed into a
	// subquery beneath the lock it would still be correct and would quietly
	// halve throughput under contention; the containers test pins the behavior,
	// and this pins the shape.
	T.Run("limits above the lock", func(t *testing.T) {
		t.Parallel()

		query := buildClaim(testTable)

		limitAt := strings.Index(query, "LIMIT $3::int")
		lockAt := strings.Index(query, "FOR UPDATE SKIP LOCKED")

		must.True(t, limitAt >= 0)
		must.True(t, lockAt >= 0)
		test.True(t, limitAt < lockAt)
	})

	// A second ordering key would let a caller jump a queue of firings that are,
	// by construction, already late.
	T.Run("orders by the oldest debt and nothing else", func(t *testing.T) {
		t.Parallel()

		query := buildClaim(testTable)

		test.True(t, strings.Contains(query, "ORDER BY s.run_at, s.timer_key"))
		test.False(t, strings.Contains(query, "priority"))
	})

	T.Run("hands back the instant that fences the firing", func(t *testing.T) {
		t.Parallel()

		query := buildClaim(testTable)

		test.True(t, strings.Contains(query, "RETURNING s.timer_key, s.payload, s.run_at"))
	})
}

func TestBuildNextDue(T *testing.T) {
	T.Parallel()

	T.Run("binds the set and the ceiling", func(t *testing.T) {
		t.Parallel()

		assertContiguousPlaceholders(t, buildNextDue(testTable), 2)
	})

	// A leased row becomes claimable when its lease lapses, not at its long-past
	// instant. Measuring to the instant instead would have a poller wake at once
	// and claim nothing for as long as a dead worker's lease had left to run.
	T.Run("measures to whichever of the instant and the lease is later", func(t *testing.T) {
		t.Parallel()

		test.True(t, strings.Contains(buildNextDue(testTable), "MIN(GREATEST(s.run_at, s.lease_until))"))
	})

	// The question is "how long until one of these becomes due", so a row
	// excluded for not being due yet would exclude the entire answer.
	T.Run("counts outstanding timers rather than due ones", func(t *testing.T) {
		t.Parallel()

		query := buildNextDue(testTable)

		test.True(t, strings.Contains(query, "s.fired_at IS NULL"))
		test.False(t, strings.Contains(query, "s.run_at <= now()"))
	})
}

func TestBuildComplete(T *testing.T) {
	T.Parallel()

	T.Run("binds the set plus a key and an instant per firing", func(t *testing.T) {
		t.Parallel()

		assertContiguousPlaceholders(t, buildComplete(testTable, 3), 7)
	})

	// The fence. Without it, a timer rescheduled during its own firing would be
	// marked fired against the schedule it no longer has.
	T.Run("matches on the instant as well as the key", func(t *testing.T) {
		t.Parallel()

		test.True(t, strings.Contains(buildComplete(testTable, 1), "(s.timer_key, s.run_at) IN ("))
	})

	// `UPDATE … WHERE timer_key IN (…)` gives Postgres no obligation to take row
	// locks in any order, so two writers with overlapping key sets can deadlock.
	T.Run("locks its rows in primary-key order", func(t *testing.T) {
		t.Parallel()

		query := buildComplete(testTable, 2)

		test.True(t, strings.Contains(query, "ORDER BY s.timer_set, s.timer_key FOR UPDATE"))
		test.False(t, strings.Contains(query, "SKIP LOCKED"))
	})

	T.Run("marks rather than deletes", func(t *testing.T) {
		t.Parallel()

		query := buildComplete(testTable, 1)

		test.True(t, strings.Contains(query, "SET fired_at = now()"))
		test.False(t, strings.Contains(query, "DELETE"))
	})
}

func TestBuildRelease(T *testing.T) {
	T.Parallel()

	T.Run("binds the set, the delay, the cause, and a key and instant per firing", func(t *testing.T) {
		t.Parallel()

		assertContiguousPlaceholders(t, buildRelease(testTable, 2), 7)
	})

	// Undoing somebody else's Complete would turn the ordinary consequence of a
	// lapsed lease into a loop.
	T.Run("skips timers that have already fired", func(t *testing.T) {
		t.Parallel()

		test.True(t, strings.Contains(buildRelease(testTable, 1), "s.fired_at IS NULL"))
	})

	// This table holds one instant per row, so a retried timer genuinely is now
	// scheduled for later rather than held behind a second column.
	T.Run("pushes the instant out rather than holding it back", func(t *testing.T) {
		t.Parallel()

		test.True(t, strings.Contains(buildRelease(testTable, 1), "run_at = now() + ($2::bigint"))
	})
}

func TestBuildCancel(T *testing.T) {
	T.Parallel()

	T.Run("binds the set and every key", func(t *testing.T) {
		t.Parallel()

		assertContiguousPlaceholders(t, buildCancel(testTable, 3), 4)
	})

	// A cancelled timer has no history worth keeping, and keeping it would make
	// Schedule distinguish reviving a cancelled timer from reviving a fired one.
	T.Run("deletes, whatever the timer's state", func(t *testing.T) {
		t.Parallel()

		query := buildCancel(testTable, 1)

		test.True(t, strings.Contains(query, "DELETE FROM"))
		test.False(t, strings.Contains(query, "fired_at IS NULL"))
		test.True(t, strings.Contains(query, "ORDER BY s.timer_set, s.timer_key FOR UPDATE"))
	})
}

func TestBuildReap(T *testing.T) {
	T.Parallel()

	T.Run("binds the set, the retention, and the batch size", func(t *testing.T) {
		t.Parallel()

		assertContiguousPlaceholders(t, buildReap(testTable), 3)
	})

	// The reaper is the one writer with nothing to prove: a row another
	// statement is holding will still be expired on the next pass.
	T.Run("skips locked rows", func(t *testing.T) {
		t.Parallel()

		query := buildReap(testTable)

		test.True(t, strings.Contains(query, "FOR UPDATE SKIP LOCKED"))
		test.True(t, strings.Contains(query, "s.fired_at IS NOT NULL"))
	})
}

func TestBuildStats(T *testing.T) {
	T.Parallel()

	T.Run("binds the set and the ceiling", func(t *testing.T) {
		t.Parallel()

		assertContiguousPlaceholders(t, buildStats(testTable), 2)
	})

	// A Stats.Due that disagreed with what Claim will actually hand out is worse
	// than no reading at all, so both are rendered from one predicate.
	T.Run("counts due timers with the claim's own predicate", func(t *testing.T) {
		t.Parallel()

		test.True(t, strings.Contains(buildStats(testTable), duePredicate("s", "$1", "$2")))
	})
}

func TestFiredTuples(T *testing.T) {
	T.Parallel()

	T.Run("numbers each pair from the given start", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, "(s.timer_key, s.run_at) IN (($4, $5::timestamptz), ($6, $7::timestamptz))",
			firedTuples(4, 2))
	})
}
