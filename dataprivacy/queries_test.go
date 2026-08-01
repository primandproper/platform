package dataprivacy

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/primandproper/platform-go/v9/database/dialect"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// The SQL these render is exercised end to end by the SQLite tests and the
// container suite. It is asserted here as well, directly, because the parts a
// single-dialect end-to-end test cannot see are exactly the parts most likely
// to be wrong: whether Postgres got numbered placeholders that match the
// arguments bound beside them, whether the row lock appears where it is
// supported and nowhere else, whether each guard clause survived an edit, and
// whether MySQL got the derived table it refuses to work without.

var testTables = newTables(DefaultTablePrefix)

// allDialects is every dialect this package emits SQL for.
var allDialects = []dialect.Dialect{dialect.Postgres, dialect.MySQL, dialect.SQLite}

// testRequest is a fully-populated request, so a builder that drops a field has
// something non-zero to drop.
func testRequest() *Request {
	completed := baseTime.Add(time.Hour)

	return &Request{
		RequestedAt:   baseTime,
		DueAt:         baseTime.Add(30 * 24 * time.Hour),
		ExpiresAt:     baseTime.Add(7 * 24 * time.Hour),
		CompletedAt:   &completed,
		ID:            "req_1",
		ArtifactRef:   "exports/req_1.json.gz",
		Subject:       Subject{ID: "user_1", Scope: "acct_1", Type: SubjectUser},
		Type:          RequestExport,
		Status:        StatusPending,
		ArtifactBytes: 4096,
		Deleted:       3,
		Anonymized:    2,
		Attempts:      1,
	}
}

// builtQuery is one rendered statement, named for the failure message.
type builtQuery struct {
	name  string
	query string
	args  []any
}

// everyQuery renders every statement this package emits, for the invariants
// that have to hold across all of them rather than within any one. Branchy
// builders appear once per branch, because the placeholder arithmetic differs
// between them and it is the arithmetic being checked.
func everyQuery(testTables *tables, d dialect.Dialect) []builtQuery {
	var (
		req      = testRequest()
		subject  = req.Subject
		ids      = []string{"req_1", "req_2"}
		failures = []byte(`{"identity":"boom"}`)
		retained = []byte(`{"invoices":"tax law"}`)
		out      []builtQuery
	)

	add := func(name, query string, args []any) {
		out = append(out, builtQuery{name: name, query: query, args: args})
	}

	query, args := testTables.buildInsertRequest(d, req, failures, retained)
	add("buildInsertRequest", query, args)

	query, args = testTables.buildSelectRequest(d, req.ID)
	add("buildSelectRequest", query, args)

	query, args = testTables.buildListRequests(d, Subject{ID: "user_1"}, "", 10, false)
	add("buildListRequests/unscoped", query, args)

	query, args = testTables.buildListRequests(d, subject, "req_0", 10, true)
	add("buildListRequests/scoped+cursor", query, args)

	query, args = testTables.buildCountRequests(d, subject)
	add("buildCountRequests", query, args)

	query, args = testTables.buildTransition(d, req.ID, []Status{StatusAwaitingConfirmation}, StatusPending, baseTime)
	add("buildTransition/non-terminal", query, args)

	query, args = testTables.buildTransition(d, req.ID, []Status{StatusAwaitingConfirmation, StatusPending}, StatusCancelled, baseTime)
	add("buildTransition/terminal", query, args)

	query, args = testTables.buildSelectClaimable(d, baseTime, 10, true)
	add("buildSelectClaimable/locked", query, args)

	query, args = testTables.buildSelectClaimable(d, baseTime, 10, false)
	add("buildSelectClaimable/unlocked", query, args)

	query, args = testTables.buildClaim(d, ids, baseTime)
	add("buildClaim", query, args)

	query, args = testTables.buildFetchByIDs(d, ids, StatusProcessing)
	add("buildFetchByIDs", query, args)

	query, args = testTables.buildCompleteExport(d, req, failures, baseTime)
	add("buildCompleteExport", query, args)

	query, args = testTables.buildCompleteErasure(d, req, failures, retained, baseTime)
	add("buildCompleteErasure", query, args)

	query, args = testTables.buildFail(d, req.ID, 2, baseTime, "boom", false)
	add("buildFail/retryable", query, args)

	query, args = testTables.buildFail(d, req.ID, 2, baseTime, "boom", true)
	add("buildFail/terminal", query, args)

	query, args = testTables.buildSelectExpiringArtifacts(d, baseTime, 10)
	add("buildSelectExpiringArtifacts", query, args)

	query, args = testTables.buildMarkExpired(d, req.ID, baseTime)
	add("buildMarkExpired", query, args)

	query, args = testTables.buildLapseUnconfirmed(d, baseTime, 10)
	add("buildLapseUnconfirmed", query, args)

	query, args = testTables.buildCountOverdue(d, baseTime)
	add("buildCountOverdue", query, args)

	query, args = testTables.buildReap(d, baseTime, 10)
	add("buildReap", query, args)

	return out
}

var pgPlaceholder = regexp.MustCompile(`\$(\d+)`)

func TestQueries_Placeholders(T *testing.T) {
	T.Parallel()

	// The invariant every builder in this file depends on and none of them
	// states: the markers rendered into the SQL are exactly $1 through $len(args),
	// each appearing at least once. A builder that skips a number binds the wrong
	// value to every marker after the gap, which the server accepts and answers
	// wrongly rather than rejecting — buildFail reaching back for len(args)-1 and
	// buildLapseUnconfirmed numbering its subquery after its SET clause are the
	// two places that arithmetic is done by hand.
	T.Run("postgres numbers every marker from one to the argument count", func(t *testing.T) {
		t.Parallel()

		for _, b := range everyQuery(testTables, dialect.Postgres) {
			seen := make(map[int]bool, len(b.args))

			for _, match := range pgPlaceholder.FindAllStringSubmatch(b.query, -1) {
				n, err := strconv.Atoi(match[1])
				must.NoError(t, err, must.Sprintf("%s: parsing placeholder %q", b.name, match[0]))
				seen[n] = true
			}

			must.MapLen(t, len(b.args), seen, must.Sprintf("%s: %s", b.name, b.query))

			for i := 1; i <= len(b.args); i++ {
				test.True(t, seen[i], test.Sprintf("%s: missing $%d in %s", b.name, i, b.query))
			}
		}
	})

	// MySQL and SQLite bind '?' positionally, so a value used in two clauses has
	// to be supplied twice. One marker too few and every argument after it lands
	// in the wrong column.
	T.Run("mysql and sqlite bind one value per marker", func(t *testing.T) {
		t.Parallel()

		for _, d := range []dialect.Dialect{dialect.MySQL, dialect.SQLite} {
			for _, b := range everyQuery(testTables, d) {
				test.EqOp(t, len(b.args), strings.Count(b.query, "?"),
					test.Sprintf("dialect %q, %s: %s", d, b.name, b.query))
			}
		}
	})

	T.Run("every statement names the configured table", func(t *testing.T) {
		t.Parallel()

		// Rendered against a non-default prefix, so a builder that reached for
		// the default name is caught rather than passing by coincidence. A
		// statement that missed the prefix would run against a table belonging to
		// another deployment sharing the schema.
		custom := newTables("custom")

		for _, d := range allDialects {
			for _, b := range everyQuery(custom, d) {
				test.StrContains(t, b.query, "custom_requests",
					test.Sprintf("dialect %q, %s: %s", d, b.name, b.query))
				test.StrNotContains(t, b.query, DefaultTablePrefix+"_requests",
					test.Sprintf("dialect %q, %s: %s", d, b.name, b.query))
			}
		}
	})
}

func TestNewTables(T *testing.T) {
	T.Parallel()

	T.Run("derives the table name from one prefix", func(t *testing.T) {
		t.Parallel()

		tables := newTables("custom")
		test.EqOp(t, "custom_requests", tables.requests)
	})

	T.Run("remembers the prefix it derived from", func(t *testing.T) {
		t.Parallel()

		// The validation runs against the prefix rather than any rendered name,
		// so the prefix has to survive the derivation.
		test.EqOp(t, "custom", newTables("custom").prefix())
	})
}

func TestBuildInsertRequest(T *testing.T) {
	T.Parallel()

	T.Run("binds one value per column", func(t *testing.T) {
		t.Parallel()

		query, args := testTables.buildInsertRequest(dialect.Postgres, testRequest(), nil, nil)

		// The column list and the argument slice are written out separately, so
		// this is the assertion that catches one of them gaining a field.
		open := strings.Index(query, "(")
		closing := strings.Index(query, ") VALUES")
		must.Less(t, closing, open)

		columns := strings.Split(query[open+1:closing], ", ")
		test.SliceLen(t, len(args), columns)
	})

	T.Run("is an insert, never an upsert", func(t *testing.T) {
		t.Parallel()

		// A resubmission is a new request with its own statutory clock. An upsert
		// would let one overwrite the requested_at that clock runs from, which is
		// the field a regulator is most likely to ask about.
		for _, d := range allDialects {
			query, _ := testTables.buildInsertRequest(d, testRequest(), nil, nil)

			test.StrHasPrefix(t, "INSERT INTO", query, test.Sprintf("dialect %q", d))
			test.StrNotContains(t, query, "ON CONFLICT", test.Sprintf("dialect %q", d))
			test.StrNotContains(t, query, "ON DUPLICATE", test.Sprintf("dialect %q", d))
			test.StrNotContains(t, query, "OR REPLACE", test.Sprintf("dialect %q", d))
		}
	})

	T.Run("seeds next_attempt with the requested time", func(t *testing.T) {
		t.Parallel()

		req := testRequest()
		_, args := testTables.buildInsertRequest(dialect.Postgres, req, nil, nil)

		// A new request is claimable immediately; a zero next_attempt would work
		// only because year 1 is in the past, and a future one would silently
		// delay every submission.
		test.Eq(t, any(req.RequestedAt.UTC()), args[10])
	})

	T.Run("maps a zero expiry and absent blobs to NULL", func(t *testing.T) {
		t.Parallel()

		req := testRequest()
		req.ExpiresAt = time.Time{}

		_, args := testTables.buildInsertRequest(dialect.Postgres, req, nil, nil)

		// A zero time bound as a value reads back as year 1, which the expiry
		// sweeps would treat as long overdue.
		test.Nil(t, args[8])
		test.Nil(t, args[16])
		test.Nil(t, args[17])
	})
}

func TestBuildSelectRequest(T *testing.T) {
	T.Parallel()

	T.Run("reads the shared projection by id", func(t *testing.T) {
		t.Parallel()

		query, args := testTables.buildSelectRequest(dialect.Postgres, "req_1")

		test.StrContains(t, query, requestColumns)
		test.StrContains(t, query, "WHERE id = $1")
		test.Eq(t, []any{"req_1"}, args)
	})
}

func TestBuildListRequests(T *testing.T) {
	T.Parallel()

	T.Run("an empty scope matches every scope", func(t *testing.T) {
		t.Parallel()

		query, args := testTables.buildListRequests(dialect.Postgres, Subject{ID: "user_1"}, "", 10, false)

		// A subject asking what has been requested in their name means all of it.
		// A listing that quietly omitted the scoped requests would be the wrong
		// answer to the only question this read exists to answer.
		//
		// subject_scope is in the projection either way; what must be absent is a
		// predicate over it.
		test.StrNotContains(t, query, "subject_scope =")
		test.Eq(t, []any{"user_1", 10}, args)
	})

	T.Run("a scope narrows to it", func(t *testing.T) {
		t.Parallel()

		subject := Subject{ID: "user_1", Scope: "acct_1"}
		query, args := testTables.buildListRequests(dialect.Postgres, subject, "", 10, false)

		test.StrContains(t, query, "subject_scope = $2")
		test.Eq(t, []any{"user_1", "acct_1", 10}, args)
	})

	T.Run("pages forward by default", func(t *testing.T) {
		t.Parallel()

		query, args := testTables.buildListRequests(dialect.Postgres, Subject{ID: "user_1"}, "req_5", 10, false)

		test.StrContains(t, query, "AND id > $2")
		test.StrContains(t, query, "ORDER BY id ASC")
		test.Eq(t, []any{"user_1", "req_5", 10}, args)
	})

	T.Run("reverses the cursor comparison when paging backward", func(t *testing.T) {
		t.Parallel()

		query, _ := testTables.buildListRequests(dialect.Postgres, Subject{ID: "user_1"}, "req_5", 10, true)

		// The comparison has to follow the order, or the second page walks away
		// from the first instead of continuing it.
		test.StrContains(t, query, "AND id < $2")
		test.StrContains(t, query, "ORDER BY id DESC")
	})

	T.Run("ignores an empty cursor", func(t *testing.T) {
		t.Parallel()

		query, args := testTables.buildListRequests(dialect.SQLite, Subject{ID: "user_1"}, "", 10, false)

		test.StrNotContains(t, query, "AND id >")
		test.SliceLen(t, 2, args)
	})

	T.Run("orders on the column the cursor names, and nothing else", func(t *testing.T) {
		t.Parallel()

		query, _ := testTables.buildListRequests(dialect.SQLite, Subject{ID: "user_1"}, "", 10, false)

		// xid string form sorts in generation order, so id order is submission
		// order. Adding requested_at to the sort would let a page boundary skip a
		// row when two requests share a timestamp.
		test.StrContains(t, query, "ORDER BY id ASC")
		test.StrNotContains(t, query, "ORDER BY requested_at")
	})

	T.Run("binds the limit last", func(t *testing.T) {
		t.Parallel()

		subject := Subject{ID: "user_1", Scope: "acct_1"}

		for _, cursor := range []string{"", "req_5"} {
			_, args := testTables.buildListRequests(dialect.Postgres, subject, cursor, 25, false)
			test.EqOp(t, 25, args[len(args)-1])
		}
	})
}

func TestBuildCountRequests(T *testing.T) {
	T.Parallel()

	T.Run("counts the same rows the listing pages over", func(t *testing.T) {
		t.Parallel()

		subject := Subject{ID: "user_1", Scope: "acct_1"}
		query, args := testTables.buildCountRequests(dialect.Postgres, subject)

		// A total that disagreed with the pages it accompanies is a pagination
		// footer that never reaches its own last page.
		test.StrContains(t, query, "COUNT(*)")
		test.StrContains(t, query, "subject_id = $1")
		test.StrContains(t, query, "subject_scope = $2")
		test.Eq(t, []any{"user_1", "acct_1"}, args)
	})

	T.Run("an empty scope counts every scope", func(t *testing.T) {
		t.Parallel()

		query, args := testTables.buildCountRequests(dialect.Postgres, Subject{ID: "user_1"})

		test.StrNotContains(t, query, "subject_scope =")
		test.Eq(t, []any{"user_1"}, args)
	})
}

func TestBuildTransition(T *testing.T) {
	T.Parallel()

	T.Run("guards on the current status in the predicate", func(t *testing.T) {
		t.Parallel()

		query, args := testTables.buildTransition(
			dialect.Postgres, "req_1", []Status{StatusAwaitingConfirmation}, StatusPending, baseTime,
		)

		// The guard being in the WHERE rather than in a read-then-write is what
		// makes Confirm safe: a subject clicking twice, or clicking as the sweeper
		// cancels the request, has the second writer match no rows and be told so,
		// instead of both succeeding and queueing the erasure twice.
		test.StrContains(t, query, "AND status IN ($4)")
		test.Eq(t, []any{"pending", baseTime.UTC(), "req_1", "awaiting_confirmation"}, args)
	})

	T.Run("accepts several origins", func(t *testing.T) {
		t.Parallel()

		query, args := testTables.buildTransition(
			dialect.Postgres, "req_1", []Status{StatusAwaitingConfirmation, StatusPending}, StatusCancelled, baseTime,
		)

		test.StrContains(t, query, "AND status IN ($4, $5)")
		test.SliceLen(t, 5, args)
	})

	T.Run("stamps completed_at only for a terminal destination", func(t *testing.T) {
		t.Parallel()

		query, _ := testTables.buildTransition(
			dialect.Postgres, "req_1", []Status{StatusAwaitingConfirmation}, StatusCancelled, baseTime,
		)

		test.StrContains(t, query, "completed_at = $2")
		test.StrNotContains(t, query, "next_attempt")
	})

	T.Run("makes a non-terminal destination claimable now", func(t *testing.T) {
		t.Parallel()

		query, _ := testTables.buildTransition(
			dialect.Postgres, "req_1", []Status{StatusAwaitingConfirmation}, StatusPending, baseTime,
		)

		// The confirmation it was waiting for has arrived; making it wait another
		// poll interval is latency spent for nothing.
		test.StrContains(t, query, "next_attempt = $2")
		test.StrNotContains(t, query, "completed_at")
	})

	T.Run("always leaves the confirmation window behind", func(t *testing.T) {
		t.Parallel()

		// Either the window was satisfied or it lapsed. A stale expires_at would
		// have the lapse sweep pick the row back up after it had already moved.
		for _, to := range []Status{StatusPending, StatusCancelled} {
			query, _ := testTables.buildTransition(
				dialect.SQLite, "req_1", []Status{StatusAwaitingConfirmation}, to, baseTime,
			)

			test.StrContains(t, query, "expires_at = NULL", test.Sprintf("destination %q", to))
			test.StrContains(t, query, "claimed_until = NULL", test.Sprintf("destination %q", to))
		}
	})
}

func TestBuildSelectClaimable(T *testing.T) {
	T.Parallel()

	T.Run("takes the row lock where the dialect has one", func(t *testing.T) {
		t.Parallel()

		for _, d := range []dialect.Dialect{dialect.Postgres, dialect.MySQL} {
			query, _ := testTables.buildSelectClaimable(d, baseTime, 10, true)

			// Without it, every worker in the fleet selects the same batch and
			// contends on the claim UPDATE instead of dividing the work.
			test.StrHasSuffix(t, " FOR UPDATE SKIP LOCKED", query, test.Sprintf("dialect %q", d))
		}
	})

	T.Run("omits the lock on SQLite, which has none", func(t *testing.T) {
		t.Parallel()

		query, _ := testTables.buildSelectClaimable(dialect.SQLite, baseTime, 10, true)
		test.StrNotContains(t, query, "FOR UPDATE")
	})

	T.Run("omits the lock when not asked for one", func(t *testing.T) {
		t.Parallel()

		for _, d := range allDialects {
			query, _ := testTables.buildSelectClaimable(d, baseTime, 10, false)
			test.StrNotContains(t, query, "FOR UPDATE", test.Sprintf("dialect %q", d))
		}
	})

	T.Run("takes only pending work that is due and unleased", func(t *testing.T) {
		t.Parallel()

		query, args := testTables.buildSelectClaimable(dialect.Postgres, baseTime, 10, false)

		// The lease check is what lets a crashed worker's requests be picked up
		// again without a second worker stealing one that is still in flight.
		test.StrContains(t, query, "status = $1 AND next_attempt <= $2")
		test.StrContains(t, query, "(claimed_until IS NULL OR claimed_until <= $3)")
		test.Eq(t, []any{"pending", baseTime.UTC(), baseTime.UTC(), 10}, args)
	})

	T.Run("serves the oldest request first", func(t *testing.T) {
		t.Parallel()

		query, _ := testTables.buildSelectClaimable(dialect.Postgres, baseTime, 10, false)

		// The statutory clock started at requested_at, so the request closest to
		// its deadline is the one to work on next.
		test.StrContains(t, query, "ORDER BY requested_at, id")
	})
}

func TestBuildClaim(T *testing.T) {
	T.Parallel()

	T.Run("charges an attempt as it leases", func(t *testing.T) {
		t.Parallel()

		query, _ := testTables.buildClaim(dialect.Postgres, []string{"req_1"}, baseTime)

		// Incrementing here rather than on failure is what bounds a request whose
		// collector reliably kills the worker: it consumes attempts even when
		// nothing gets far enough to record a failure.
		test.StrContains(t, query, "attempts = attempts + 1")
	})

	T.Run("re-checks the status it just selected on", func(t *testing.T) {
		t.Parallel()

		query, args := testTables.buildClaim(dialect.Postgres, []string{"req_1", "req_2"}, baseTime)

		// Between the SELECT and this UPDATE a subject may have cancelled.
		test.StrContains(t, query, "WHERE id IN ($3, $4) AND status = $5")
		test.Eq(t, []any{"processing", baseTime.UTC(), "req_1", "req_2", "pending"}, args)
	})
}

func TestBuildFetchByIDs(T *testing.T) {
	T.Parallel()

	T.Run("projects the claimed batch in a stable order", func(t *testing.T) {
		t.Parallel()

		query, args := testTables.buildFetchByIDs(dialect.Postgres, []string{"req_1", "req_2"}, StatusProcessing)

		test.StrContains(t, query, requestColumns)
		test.StrContains(t, query, "WHERE id IN ($1, $2) AND status = $3")
		test.StrHasSuffix(t, "ORDER BY requested_at, id", query)
		test.Eq(t, []any{"req_1", "req_2", "processing"}, args)
	})
}

func TestBuildCompleteExport(T *testing.T) {
	T.Parallel()

	T.Run("records where the artifact went and when it expires", func(t *testing.T) {
		t.Parallel()

		req := testRequest()
		query, args := testTables.buildCompleteExport(dialect.Postgres, req, nil, baseTime)

		test.StrContains(t, query, "artifact_ref = $5")
		test.StrContains(t, query, "artifact_bytes = $6")
		test.Eq(t, any(req.ExpiresAt.UTC()), args[3])
		test.EqOp(t, any(req.ArtifactRef), args[4])
		test.EqOp(t, any(req.ArtifactBytes), args[5])
	})

	T.Run("cannot resurrect a request that left processing", func(t *testing.T) {
		t.Parallel()

		query, args := testTables.buildCompleteExport(dialect.Postgres, testRequest(), nil, baseTime)

		// Exactly what a long export racing the lapse sweep would otherwise do.
		test.StrHasSuffix(t, "WHERE id = $8 AND status = $9", query)
		test.EqOp(t, "processing", args[8])
	})

	T.Run("releases the lease and clears the previous error", func(t *testing.T) {
		t.Parallel()

		query, _ := testTables.buildCompleteExport(dialect.Postgres, testRequest(), nil, baseTime)

		// A retry that succeeds must not leave the error from the attempt before
		// it sitting in the row a subject is shown.
		test.StrContains(t, query, "claimed_until = NULL")
		test.StrContains(t, query, "last_error = ''")
	})

	T.Run("stores an empty failure set as NULL", func(t *testing.T) {
		t.Parallel()

		_, args := testTables.buildCompleteExport(dialect.Postgres, testRequest(), nil, baseTime)
		test.Nil(t, args[6])
	})
}

func TestBuildCompleteErasure(T *testing.T) {
	T.Parallel()

	T.Run("records what was destroyed, anonymized, and kept", func(t *testing.T) {
		t.Parallel()

		req := testRequest()
		retained := []byte(`{"invoices":"tax law"}`)

		query, args := testTables.buildCompleteErasure(dialect.Postgres, req, nil, retained, baseTime)

		test.StrContains(t, query, "deleted_rows = $4")
		test.StrContains(t, query, "anonymized_rows = $5")
		test.StrContains(t, query, "retained = $7")
		test.EqOp(t, any(req.Deleted), args[3])
		test.EqOp(t, any(req.Anonymized), args[4])
		test.Eq(t, any(retained), args[6])
	})

	T.Run("clears the expiry rather than setting one", func(t *testing.T) {
		t.Parallel()

		query, _ := testTables.buildCompleteErasure(dialect.Postgres, testRequest(), nil, nil, baseTime)

		// An erasure has no artifact to expire, and the column held its
		// confirmation window. Leaving that behind would have the lapse sweep
		// cancel a request that has already run.
		test.StrContains(t, query, "expires_at = NULL")
	})

	T.Run("writes no artifact columns", func(t *testing.T) {
		t.Parallel()

		query, _ := testTables.buildCompleteErasure(dialect.Postgres, testRequest(), nil, nil, baseTime)

		// An erasure produces no object; a stale ref here would keep the reaper
		// away from the row forever.
		test.StrNotContains(t, query, "artifact_ref =")
		test.StrNotContains(t, query, "artifact_bytes =")
	})

	T.Run("guards on processing like the export path", func(t *testing.T) {
		t.Parallel()

		query, args := testTables.buildCompleteErasure(dialect.Postgres, testRequest(), nil, nil, baseTime)

		test.StrHasSuffix(t, "WHERE id = $8 AND status = $9", query)
		test.EqOp(t, "processing", args[8])
	})
}

func TestBuildFail(T *testing.T) {
	T.Parallel()

	T.Run("returns a retryable failure to pending", func(t *testing.T) {
		t.Parallel()

		next := baseTime.Add(time.Minute)
		query, args := testTables.buildFail(dialect.Postgres, "req_1", 2, next, "boom", false)

		test.EqOp(t, "pending", args[0])
		test.StrNotContains(t, query, "completed_at")
		test.StrHasSuffix(t, "WHERE id = $5 AND status = $6", query)
		test.Eq(t, []any{"pending", 2, next.UTC(), "boom", "req_1", "processing"}, args)
	})

	T.Run("gives up and stamps completed_at when terminal", func(t *testing.T) {
		t.Parallel()

		query, args := testTables.buildFail(dialect.Postgres, "req_1", 3, baseTime, "boom", true)

		test.EqOp(t, "failed", args[0])
		test.StrContains(t, query, "completed_at = $5")

		// The extra SET shifts the guard's markers along by one; getting this
		// wrong binds the request ID to the status column.
		test.StrHasSuffix(t, "WHERE id = $6 AND status = $7", query)
		test.EqOp(t, "req_1", args[5])
		test.EqOp(t, "processing", args[6])
	})

	T.Run("writes the attempt count it was given", func(t *testing.T) {
		t.Parallel()

		_, args := testTables.buildFail(dialect.Postgres, "req_1", 1, baseTime, "boom", false)

		// Written rather than left as the claim incremented it, so a caller can
		// decline to charge an attempt for a failure the request never caused.
		test.EqOp(t, 1, args[1])
	})

	T.Run("always releases the lease", func(t *testing.T) {
		t.Parallel()

		for _, terminal := range []bool{false, true} {
			query, _ := testTables.buildFail(dialect.SQLite, "req_1", 1, baseTime, "boom", terminal)

			// A failure that kept the lease would leave the request unclaimable
			// until the lease expired, on top of having failed.
			test.StrContains(t, query, "claimed_until = NULL", test.Sprintf("terminal %t", terminal))
		}
	})
}

func TestBuildSelectExpiringArtifacts(T *testing.T) {
	T.Parallel()

	T.Run("reads rather than writes", func(t *testing.T) {
		t.Parallel()

		query, _ := testTables.buildSelectExpiringArtifacts(dialect.Postgres, baseTime, 10)

		// The object has to go before the row says it has. A bulk UPDATE marking
		// rows expired would be one round trip and would leave every artifact in
		// the bucket — precisely the outcome the expiry state exists to prevent.
		test.StrHasPrefix(t, "SELECT", query)
		test.StrContains(t, query, requestColumns)
	})

	T.Run("skips completed rows that have no artifact", func(t *testing.T) {
		t.Parallel()

		query, args := testTables.buildSelectExpiringArtifacts(dialect.Postgres, baseTime, 10)

		// Erasures and already-expired exports both sit in the table with an
		// empty ref; sweeping them would delete nothing, forever.
		test.StrContains(t, query, "artifact_ref <> ''")
		test.StrContains(t, query, "expires_at IS NOT NULL")
		test.Eq(t, []any{"completed", baseTime.UTC(), 10}, args)
	})
}

func TestBuildMarkExpired(T *testing.T) {
	T.Parallel()

	T.Run("clears the reference as it retires the row", func(t *testing.T) {
		t.Parallel()

		query, args := testTables.buildMarkExpired(dialect.Postgres, "req_1", baseTime)

		// A stale path must not outlive the object it named and be handed to a
		// signer later.
		test.StrContains(t, query, "artifact_ref = ''")
		test.Eq(t, []any{"expired", baseTime.UTC(), "req_1", "completed"}, args)
	})

	T.Run("moves only a completed request", func(t *testing.T) {
		t.Parallel()

		query, _ := testTables.buildMarkExpired(dialect.Postgres, "req_1", baseTime)

		// expired is reachable from completed and nowhere else.
		test.StrHasSuffix(t, "WHERE id = $3 AND status = $4", query)
	})
}

func TestBuildLapseUnconfirmed(T *testing.T) {
	T.Parallel()

	T.Run("binds every value once, in the order its marker is rendered", func(t *testing.T) {
		t.Parallel()

		query, args := testTables.buildLapseUnconfirmed(dialect.Postgres, baseTime, 10)

		// The SET clause is rendered before the subquery but numbered ahead of
		// it, so the arguments are not in source order. On MySQL and SQLite,
		// where markers are positional, now has to appear twice.
		test.StrContains(t, query, "status = $1, completed_at = $2")
		test.StrContains(t, query, "WHERE status = $3 AND expires_at IS NOT NULL AND expires_at <= $4")
		test.StrContains(t, query, "LIMIT $5")
		test.Eq(t, []any{"cancelled", baseTime.UTC(), "awaiting_confirmation", baseTime.UTC(), 10}, args)
	})

	T.Run("materializes the subquery for MySQL only", func(t *testing.T) {
		t.Parallel()

		// MySQL refuses a subquery that reads the table being updated
		// (ER_UPDATE_TABLE_USED), and accepts it once wrapped in a derived table.
		my, _ := testTables.buildLapseUnconfirmed(dialect.MySQL, baseTime, 10)
		test.StrContains(t, my, "AS lapsed")

		for _, d := range []dialect.Dialect{dialect.Postgres, dialect.SQLite} {
			query, _ := testTables.buildLapseUnconfirmed(d, baseTime, 10)
			test.StrNotContains(t, query, "AS lapsed", test.Sprintf("dialect %q", d))
		}
	})

	T.Run("clears the window it acted on", func(t *testing.T) {
		t.Parallel()

		query, _ := testTables.buildLapseUnconfirmed(dialect.SQLite, baseTime, 10)

		// Otherwise the next sweep selects the same rows and rewrites them every
		// time it runs.
		test.StrContains(t, query, "expires_at = NULL")
	})
}

func TestBuildCountOverdue(T *testing.T) {
	T.Parallel()

	T.Run("counts only what is still owed to somebody", func(t *testing.T) {
		t.Parallel()

		query, args := testTables.buildCountOverdue(dialect.Postgres, baseTime)

		// A request that completed after its deadline is late, not overdue.
		// Including the terminal statuses would make the gauge climb forever and
		// never come back down.
		test.StrContains(t, query, "status IN ($1, $2, $3) AND due_at < $4")
		test.Eq(t, []any{"awaiting_confirmation", "pending", "processing", baseTime.UTC()}, args)
	})

	T.Run("breaks the gauge down by request type", func(t *testing.T) {
		t.Parallel()

		query, _ := testTables.buildCountOverdue(dialect.Postgres, baseTime)

		// Exports and erasures have different response windows and different
		// remedies; one number covering both is not actionable.
		test.StrContains(t, query, "SELECT request_type, COUNT(*)")
		test.StrHasSuffix(t, "GROUP BY request_type", query)
	})
}

func TestBuildReap(T *testing.T) {
	T.Parallel()

	T.Run("never reaps a row that still names an artifact", func(t *testing.T) {
		t.Parallel()

		query, _ := testTables.buildReap(dialect.Postgres, baseTime, 10)

		// The reference is the only record of where that object is. Deleting the
		// row first would leave a file containing everything known about a person
		// in a bucket with nothing pointing at it — worse than the row this was
		// cleaning up.
		test.StrContains(t, query, "artifact_ref = ''")
	})

	T.Run("reaps only settled rows past the retention window", func(t *testing.T) {
		t.Parallel()

		query, args := testTables.buildReap(dialect.Postgres, baseTime, 10)

		test.StrHasPrefix(t, "DELETE FROM", query)
		test.StrContains(t, query, "status IN ($1, $2, $3, $4)")
		test.StrContains(t, query, "completed_at IS NOT NULL AND completed_at < $5")
		test.Eq(t, []any{"completed", "failed", "expired", "cancelled", baseTime.UTC(), 10}, args)
	})

	T.Run("materializes the subquery for MySQL only", func(t *testing.T) {
		t.Parallel()

		my, _ := testTables.buildReap(dialect.MySQL, baseTime, 10)
		test.StrContains(t, my, "AS doomed")

		for _, d := range []dialect.Dialect{dialect.Postgres, dialect.SQLite} {
			query, _ := testTables.buildReap(d, baseTime, 10)
			test.StrNotContains(t, query, "AS doomed", test.Sprintf("dialect %q", d))
		}
	})
}

func TestStatusSets(T *testing.T) {
	T.Parallel()

	T.Run("active and terminal partition the status space", func(t *testing.T) {
		t.Parallel()

		// The overdue gauge reads one set and the reaper the other. A status
		// missing from both is never counted and never cleaned up; one in both
		// would be reaped while still owed.
		seen := make(map[Status]int, len(activeStatuses)+len(terminalStatuses))

		for _, s := range activeStatuses {
			test.False(t, s.Terminal(), test.Sprintf("status %q", s))
			seen[s]++
		}

		for _, s := range terminalStatuses {
			test.True(t, s.Terminal(), test.Sprintf("status %q", s))
			seen[s]++
		}

		for _, s := range []Status{
			StatusAwaitingConfirmation, StatusPending, StatusProcessing,
			StatusCompleted, StatusFailed, StatusExpired, StatusCancelled,
		} {
			test.EqOp(t, 1, seen[s], test.Sprintf("status %q", s))
		}
	})
}

func TestBlobOrNil(T *testing.T) {
	T.Parallel()

	T.Run("maps an empty encoding to NULL", func(t *testing.T) {
		t.Parallel()

		// "No failures" and "an empty failure map" mean the same thing, and
		// storing two renderings of it would make the round trip depend on which
		// call site wrote the row.
		test.Nil(t, blobOrNil(nil))
		test.Nil(t, blobOrNil([]byte{}))
	})

	T.Run("passes a populated encoding through", func(t *testing.T) {
		t.Parallel()

		encoded := []byte(`{"identity":"boom"}`)
		test.Eq(t, any(encoded), blobOrNil(encoded))
	})
}

func TestNullableTime(T *testing.T) {
	T.Parallel()

	T.Run("maps the zero time to NULL", func(t *testing.T) {
		t.Parallel()

		// Bound as a value it reads back as year 1, which every comparison in the
		// expiry sweeps would treat as long overdue.
		test.Nil(t, nullableTime(time.Time{}))
	})

	T.Run("normalizes to UTC", func(t *testing.T) {
		t.Parallel()

		local := baseTime.In(time.FixedZone("UTC+2", 2*60*60))

		got, ok := nullableTime(local).(time.Time)
		must.True(t, ok)

		// SQLite compares these as the strings Go renders them into, and that
		// ordering is only chronological while every value is UTC.
		test.EqOp(t, time.UTC, got.Location())
		test.True(t, got.Equal(baseTime))
	})
}
