package webhooks

import (
	"strings"
	"testing"
	"time"

	"github.com/primandproper/platform-go/v13/database/dialect"
	"github.com/primandproper/platform-go/v13/tenancy"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// allDialects is every dialect this package's schema and statements claim.
var allDialects = []dialect.Dialect{dialect.Postgres, dialect.MySQL, dialect.SQLite}

// These tests read rendered SQL. Whether a server accepts it is containers_test's
// question; what they pin is the half that is silently wrong rather than loudly
// wrong — a column named one thing in the schema and another in the statement.

func TestBuildUpsertEndpoint_StampsTheLastMutation(T *testing.T) {
	T.Parallel()

	now := time.Date(2026, time.August, 27, 9, 0, 0, 0, time.UTC)

	endpoint := &Endpoint{
		ID: "e1", Scope: tenancy.Of("acct_1"), URL: "https://example.test/hook",
		ContentType: "application/json",
		Secret:      Secret{Current: []byte("k")},
	}

	for _, d := range allDialects {
		query, args := newTables("").buildUpsertEndpoint(d, endpoint, []byte("{}"), now)

		// The insert half writes created_at and leaves the last-mutation stamp
		// unset: an endpoint nobody has re-registered has not been updated.
		inserted, _, found := strings.Cut(query, " VALUES ")
		must.True(T, found, must.Sprintf("dialect %q", d))
		test.StrContains(T, inserted, "created_at", test.Sprintf("dialect %q", d))
		test.StrNotContains(T, inserted, "last_updated_at", test.Sprintf("dialect %q", d))

		// The conflict half is the update, so it stamps.
		test.StrContains(T, query, "last_updated_at = ", test.Sprintf("dialect %q", d))
		test.SliceLen(T, 12, args, test.Sprintf("dialect %q", d))
	}
}

// The delivery log's creation column is created_at rather than a domain name for
// the same instant, which is what makes the table one querygen can read a shape
// from. The insert and the listing have to agree about it, and they cannot
// disagree while both are rendered from attemptColumns.
func TestAttemptStatements_UseTheConventionalCreationColumn(T *testing.T) {
	T.Parallel()

	at := time.Date(2026, time.August, 27, 9, 0, 0, 0, time.UTC)

	test.StrContains(T, attemptColumns, "created_at")
	test.StrNotContains(T, attemptColumns, "attempted_at")

	for _, d := range allDialects {
		insert, args := newTables("").buildInsertAttempt(d, &Attempt{
			ID: "a1", DeliveryID: "d1", EndpointID: "e1",
			AttemptCount: 1, StatusCode: 200, CreatedAt: at,
		})

		test.StrContains(T, insert, attemptColumns, test.Sprintf("dialect %q", d))
		test.EqOp(T, any(at), args[len(args)-1], test.Sprintf("dialect %q", d))

		list, _ := newTables("").buildListAttempts(d, tenancy.Of("acct_1"), "d1", "", 10)
		test.StrContains(T, list, "ORDER BY a.created_at, a.id", test.Sprintf("dialect %q", d))
	}
}
