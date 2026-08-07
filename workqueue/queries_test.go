package workqueue

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

const testTable = "work_queue_items"

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

		test.EqOp(t, "work_queue_items", tableFor(""))
	})

	T.Run("a namespace is separated by the renderer", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, "ddb_work_queue_items", tableFor("ddb"))
	})
}

func TestBuildUpsert(T *testing.T) {
	T.Parallel()

	T.Run("binds four parameters per row", func(t *testing.T) {
		t.Parallel()

		rows := []encodedEntry{
			{key: "a", priority: 1, delayMicros: 0},
			{key: "b", priority: 0, delayMicros: 5},
		}

		query, args := buildUpsert(testTable, "jobs", rows)

		must.SliceLen(t, 8, args)
		assertContiguousPlaceholders(t, query, 8)
		test.EqOp(t, "jobs", args[0])
		test.EqOp(t, "a", args[1])
		test.EqOp(t, "b", args[5])
	})

	// Availability and priority are one-way for an outstanding item and outright
	// for a completed one. Both halves matter: without the first, a late quiet
	// enqueue demotes work somebody already flagged as urgent; without the
	// second, restarting a completed item would inherit an availability from the
	// past and ignore the delay the caller asked for.
	T.Run("re-enqueueing only raises priority and only brings availability forward", func(t *testing.T) {
		t.Parallel()

		query, _ := buildUpsert(testTable, "jobs", []encodedEntry{{key: "a"}})

		test.True(t, strings.Contains(query, "priority = GREATEST(q.priority, excluded.priority)"))
		test.True(t, strings.Contains(query, "LEAST(q.available_at, excluded.available_at)"))
		test.True(t, strings.Contains(query, "ELSE excluded.available_at END"))
	})

	// Enqueueing an item somebody is working on right now must not revoke their
	// lease.
	T.Run("leaves an outstanding lease alone", func(t *testing.T) {
		t.Parallel()

		query, _ := buildUpsert(testTable, "jobs", []encodedEntry{{key: "a"}})

		_, conflict, found := strings.Cut(query, "DO UPDATE")
		must.True(t, found)
		test.False(t, strings.Contains(conflict, "lease_until"))
	})

	T.Run("restarting a completed item resets its attempts", func(t *testing.T) {
		t.Parallel()

		query, _ := buildUpsert(testTable, "jobs", []encodedEntry{{key: "a"}})

		test.True(t, strings.Contains(query, "attempts = CASE WHEN q.completed_at IS NULL THEN q.attempts ELSE 0 END"))
		test.True(t, strings.Contains(query, "completed_at = NULL"))
	})
}

func TestBuildClaim(T *testing.T) {
	T.Parallel()

	query := buildClaim(testTable)

	T.Run("claims with SKIP LOCKED", func(t *testing.T) {
		t.Parallel()

		test.True(t, strings.Contains(query, "FOR UPDATE SKIP LOCKED"))
	})

	T.Run("orders by priority, then waiting time, then key", func(t *testing.T) {
		t.Parallel()

		test.True(t, strings.Contains(query, "ORDER BY q.priority DESC, q.available_at, q.item_key"))
	})

	// Selecting and leasing in one statement is what makes a claim atomic: there
	// is no window in which an item has been chosen but not yet leased, so two
	// claimers cannot both see it.
	T.Run("selects and leases in one statement", func(t *testing.T) {
		t.Parallel()

		test.True(t, strings.HasPrefix(query, "WITH due AS ("))
		test.True(t, strings.Contains(query, "SET lease_until = now() +"))
		test.True(t, strings.Contains(query, "attempts = q.attempts + 1"))
	})

	T.Run("every scheduling comparison is against the server's now()", func(t *testing.T) {
		t.Parallel()

		test.True(t, strings.Contains(query, "q.lease_until <= now()"))
		test.True(t, strings.Contains(query, "q.available_at <= now()"))
	})

	T.Run("reports whether the prior lease had lapsed", func(t *testing.T) {
		t.Parallel()

		test.True(t, strings.Contains(query, "due.prior_lease > TIMESTAMPTZ 'epoch'"))
	})

	T.Run("binds queue, ceiling, limit, and lease", func(t *testing.T) {
		t.Parallel()

		assertContiguousPlaceholders(t, query, 4)
	})
}

// The lock ordering is the package's headline claim, so it is asserted on every
// writer that takes locks without SKIP LOCKED rather than on one of them.
func TestKeyedWriters_OrderTheirLocks(T *testing.T) {
	T.Parallel()

	for name, query := range map[string]string{
		"complete": buildComplete(testTable, 3),
		"release":  buildRelease(testTable, 3),
		"remove":   buildRemove(testTable, 3),
	} {
		T.Run(name, func(t *testing.T) {
			t.Parallel()

			test.True(t, strings.Contains(query, "ORDER BY q.queue_name, q.item_key FOR UPDATE"),
				test.Sprintf("query: %s", query))
			test.False(t, strings.Contains(query, "SKIP LOCKED"))
		})
	}
}

func TestBuildComplete(T *testing.T) {
	T.Parallel()

	query := buildComplete(testTable, 2)

	T.Run("binds the queue and every key", func(t *testing.T) {
		t.Parallel()

		assertContiguousPlaceholders(t, query, 3)
	})

	// Marked, not deleted: a duplicate or a gap has to be investigable
	// afterwards, and the reaper is what eventually removes the row.
	T.Run("marks rather than deletes", func(t *testing.T) {
		t.Parallel()

		test.True(t, strings.Contains(query, "SET completed_at = now()"))
		test.False(t, strings.Contains(query, "DELETE"))
	})
}

func TestBuildRelease(T *testing.T) {
	T.Parallel()

	query := buildRelease(testTable, 2)

	T.Run("binds the queue, the delay, the cause, and every key", func(t *testing.T) {
		t.Parallel()

		assertContiguousPlaceholders(t, query, 5)
	})

	// A late release arriving after somebody else finished the work must not
	// undo their completion, or the pair would loop.
	T.Run("skips already-completed items", func(t *testing.T) {
		t.Parallel()

		test.True(t, strings.Contains(query, "q.completed_at IS NULL"))
	})

	T.Run("holds the item for the bound delay", func(t *testing.T) {
		t.Parallel()

		test.True(t, strings.Contains(query, "available_at = now() + ($2::bigint * INTERVAL '1 microsecond')"))
	})
}

func TestBuildRemove(T *testing.T) {
	T.Parallel()

	query := buildRemove(testTable, 1)

	T.Run("deletes regardless of lease", func(t *testing.T) {
		t.Parallel()

		test.True(t, strings.Contains(query, "DELETE FROM "+testTable))
		test.False(t, strings.Contains(query, "lease_until"))
	})
}

func TestBuildReap(T *testing.T) {
	T.Parallel()

	query := buildReap(testTable)

	T.Run("removes only completed rows past retention", func(t *testing.T) {
		t.Parallel()

		test.True(t, strings.Contains(query, "q.completed_at IS NOT NULL"))
		test.True(t, strings.Contains(query, "q.completed_at < now() - ($2::bigint * INTERVAL '1 microsecond')"))
	})

	// The reaper is the one writer with nothing to prove: a row somebody else is
	// holding is still expired on the next pass, so it skips rather than waits.
	T.Run("skips locked rows", func(t *testing.T) {
		t.Parallel()

		test.True(t, strings.Contains(query, "FOR UPDATE SKIP LOCKED"))
	})

	T.Run("binds the queue, the retention, and the batch size", func(t *testing.T) {
		t.Parallel()

		assertContiguousPlaceholders(t, query, 3)
	})
}

func TestBuildStats(T *testing.T) {
	T.Parallel()

	query := buildStats(testTable)

	T.Run("binds the queue and the attempt ceiling", func(t *testing.T) {
		t.Parallel()

		assertContiguousPlaceholders(t, query, 2)
	})

	// A Ready that disagreed with what Claim will actually hand out would be
	// worse than no reading at all, so both are rendered from one predicate.
	T.Run("counts ready with the claim's own predicate", func(t *testing.T) {
		t.Parallel()

		ready := claimablePredicate("q", "$1", "$2")

		test.True(t, strings.Contains(query, ready))
		test.True(t, strings.Contains(buildClaim(testTable), ready))
	})

	T.Run("measures the oldest ready age against the server's now()", func(t *testing.T) {
		t.Parallel()

		test.True(t, strings.Contains(query, "EXTRACT(EPOCH FROM (now() - MIN("))
	})
}

// No statement in this package may bind a timestamp or return one. Durations
// cross the seam instead, so no caller's clock can enter the schedule — and that
// property is easy to lose one statement at a time.
func TestQueries_BindNoTimestamps(T *testing.T) {
	T.Parallel()

	upsert, _ := buildUpsert(testTable, "jobs", []encodedEntry{{key: "a"}})

	for name, query := range map[string]string{
		"upsert":   upsert,
		"claim":    buildClaim(testTable),
		"complete": buildComplete(testTable, 1),
		"release":  buildRelease(testTable, 1),
		"remove":   buildRemove(testTable, 1),
		"reap":     buildReap(testTable),
		"stats":    buildStats(testTable),
	} {
		T.Run(name, func(t *testing.T) {
			t.Parallel()

			// Every timestamp the statement writes or compares is either now(),
			// an offset from it, or the epoch sentinel — never a bound value.
			for _, marker := range placeholderPattern.FindAllString(query, -1) {
				test.False(t, strings.Contains(query, marker+"::timestamptz"),
					test.Sprintf("%s binds a timestamp at %s", name, marker))
			}

			test.False(t, strings.Contains(query, "TIMESTAMPTZ $"))
		})
	}
}
