package querygen

import (
	"strings"
	"testing"

	"github.com/primandproper/platform-go/v13/database/dialect"
	platformerrors "github.com/primandproper/platform-go/v13/errors"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// The fixture is the container suite's — a membership keying a crew to a person,
// and the people it names, which is identity's roster with the names filed off.
// One vocabulary for the rendered assertions and the executed ones, so a shape
// pinned here is the shape a server was asked about.

// rosterJunction projects the person beside the membership, which is what the
// roster shape is for.
func rosterJunction() *Junction {
	return &Junction{
		Table:    peopleTable,
		Column:   IDColumn,
		OnColumn: personColumn,
		Columns:  personColumns(),
		Prefix:   "person",
	}
}

// TestGenerator_JunctionListQuery_CanonicalForm pins the statement, in full, for
// the smallest column set that still exercises every part of it.
//
// A golden rather than a set of substring assertions, because the parts that go
// wrong in a two-table read are the ones a substring cannot see: which table the
// cursor pages over, which side of the join the archived toggle applies to, and
// whether the counts read over the same FROM clause the page does. All three are
// visible here by reading the statement, and a change to any of them is a diff
// somebody has to justify.
func TestGenerator_JunctionListQuery_CanonicalForm(t *testing.T) {
	t.Parallel()

	const want = `-- name: ListCrewMembers :many
SELECT
	crew_members.id,
	crew_members.belongs_to_crew,
	crew_members.archived_at,
	people.id AS person_id,
	people.name AS person_name,
	people.archived_at AS person_archived_at,
	(
		SELECT COUNT(crew_members.id)
		FROM crew_members
		JOIN people ON crew_members.belongs_to_person=people.id
		WHERE (COALESCE(sqlc.narg(include_archived), false) = true OR crew_members.archived_at IS NULL)
			AND crew_members.belongs_to_crew = sqlc.arg(belongs_to_crew)
			AND people.archived_at IS NULL
	) AS filtered_count,
	(
		SELECT COUNT(crew_members.id)
		FROM crew_members
		JOIN people ON crew_members.belongs_to_person=people.id
		WHERE (COALESCE(sqlc.narg(include_archived), false) = true OR crew_members.archived_at IS NULL)
			AND crew_members.belongs_to_crew = sqlc.arg(belongs_to_crew)
			AND people.archived_at IS NULL
	) AS total_count
FROM crew_members
JOIN people ON crew_members.belongs_to_person=people.id
WHERE (COALESCE(sqlc.narg(include_archived), false) = true OR crew_members.archived_at IS NULL)
	AND crew_members.belongs_to_crew = sqlc.arg(belongs_to_crew)
	AND people.archived_at IS NULL
	AND crew_members.id > COALESCE(sqlc.narg(page_cursor), '')
ORDER BY crew_members.id ASC
LIMIT COALESCE(sqlc.narg(result_limit), 50);
`

	// The smallest column set on both sides — no created_at and no
	// last_updated_at — so what the golden shows is the join, the aliasing and
	// the counts rather than four lines of window predicate repeated three
	// times. The window itself is the single-table list's and is pinned there.
	queries := For(dialect.SQLite).JunctionListQueries("ListCrewMembers", crewMembersTable,
		[]string{IDColumn, crewColumn, ArchivedAtColumn},
		&Junction{
			Table:    peopleTable,
			Column:   IDColumn,
			OnColumn: personColumn,
			Columns:  []string{IDColumn, "name", ArchivedAtColumn},
			Prefix:   "person",
		},
		Match{Column: crewColumn})

	must.SliceLen(t, 2, queries)
	test.EqOp(t, want, pagedList(queries, Ascending).Render())

	// The descending half differs in the two lines a direction is: the cursor
	// comparison and the ORDER BY. Pinning it as the ascending statement with
	// those two lines substituted is what says so — a change reaching any other
	// line of it fails here rather than in whichever store noticed first.
	descending := strings.Replace(
		strings.Replace(want,
			"-- name: ListCrewMembers :many",
			"-- name: ListCrewMembersDescending :many", 1),
		"\tAND crew_members.id > COALESCE(sqlc.narg(page_cursor), '')\nORDER BY crew_members.id ASC\n",
		"\tAND (crew_members.id <= COALESCE(sqlc.narg(page_cursor), crew_members.id) AND crew_members.id <> COALESCE(sqlc.narg(page_cursor), ''))\nORDER BY crew_members.id DESC\n", 1)

	test.EqOp(t, descending, pagedList(queries, Descending).Render())
}

// pagedList picks one direction out of an emitted pair, for the assertions that
// are about everything else the statement carries — the join, the aliasing, the
// counts — which the two halves share.
func pagedList(queries []*Query, direction Direction) *Query {
	if direction == Descending {
		return queries[1]
	}

	return queries[0]
}

// TestGenerator_JunctionListAllQuery_CanonicalForm pins the unpaged form, which
// is the paged one with everything a page implies removed.
func TestGenerator_JunctionListAllQuery_CanonicalForm(t *testing.T) {
	t.Parallel()

	const want = `-- name: ListCrewsForPerson :many
SELECT
	crew_members.id,
	crew_members.belongs_to_crew,
	crew_members.belongs_to_person,
	crew_members.archived_at
FROM crew_members
WHERE crew_members.archived_at IS NULL
	AND crew_members.belongs_to_person = sqlc.arg(belongs_to_person)
ORDER BY crew_members.default_crew DESC, crew_members.belongs_to_crew ASC;
`

	query := For(dialect.SQLite).JunctionListAllQuery("ListCrewsForPerson", crewMembersTable,
		[]string{IDColumn, crewColumn, personColumn, ArchivedAtColumn},
		nil,
		[]Order{{Column: "default_crew", Descending: true}, {Column: crewColumn}},
		Match{Column: personColumn})

	test.EqOp(t, want, query.Render())
}

// TestGenerator_JunctionListQuery_IsTheListStatement pins the property that made
// the join a parameter of listStatement rather than a statement of its own: a
// junction list with no junction is byte-for-byte the list query, so a keyed
// two-table read cannot come to filter differently from a keyed one-table read.
func TestGenerator_JunctionListQuery_IsTheListStatement(T *testing.T) {
	T.Parallel()

	for _, d := range []dialect.Dialect{dialect.Postgres, dialect.MySQL, dialect.SQLite} {
		T.Run(string(d), func(t *testing.T) {
			t.Parallel()

			match := Match{Column: crewColumn}

			joined := For(d).JunctionListQueries("ListCrews", crewMembersTable, crewMemberColumns(), nil, match)
			plain := For(d).ListQueries("ListCrews", crewMembersTable, crewMemberColumns(), match)

			must.SliceLen(t, 2, joined)
			must.SliceLen(t, 2, plain)

			// Both directions, so a junction list cannot come to filter
			// differently from a keyed one-table read in either of them.
			for i := range plain {
				test.EqOp(t, plain[i].Content, joined[i].Content)
				test.EqOp(t, plain[i].Annotation, joined[i].Annotation)
			}
		})
	}
}

// TestGenerator_JunctionListQuery_TheJoinIsInEveryFrom is the assertion the
// golden above cannot make for the dialects it does not render: a count taken
// over a FROM clause the page does not use is a number describing a collection
// nobody asked about, and there are three FROM clauses in this statement.
func TestGenerator_JunctionListQuery_TheJoinIsInEveryFrom(T *testing.T) {
	T.Parallel()

	for _, d := range []dialect.Dialect{dialect.Postgres, dialect.MySQL, dialect.SQLite} {
		T.Run(string(d), func(t *testing.T) {
			t.Parallel()

			content := pagedList(For(d).JunctionListQueries("ListCrewMembers", crewMembersTable,
				crewMemberColumns(), rosterJunction(), Match{Column: crewColumn}), Ascending).Content

			const join = "JOIN people ON crew_members.belongs_to_person=people.id"

			test.EqOp(t, 3, strings.Count(content, join))
			test.EqOp(t, 3, strings.Count(content, "FROM "+crewMembersTable))
		})
	}
}

// TestJunction_ProjectionIsAliased pins what the Prefix is for. Two tables
// following these conventions share most of their column names, and an
// unaliased projection of both has two columns called id — so the projected
// half is aliased and the listed half is not.
func TestJunction_ProjectionIsAliased(t *testing.T) {
	t.Parallel()

	content := pagedList(For(dialect.Postgres).JunctionListQueries("ListCrewMembers", crewMembersTable,
		crewMemberColumns(), rosterJunction(), Match{Column: crewColumn}), Ascending).Content

	for _, projected := range []string{"person_id", "person_name", "person_archived_at"} {
		test.StrContains(t, content, projected)
	}

	// The listed table's own columns keep their names, because they are the
	// ones a single-table read of the same table already produces.
	test.StrContains(t, content, "\tcrew_members.id,\n")
	test.StrNotContains(t, content, "AS crew_members_id")
}

// TestJunction_PredicatesWithoutAProjection is the other half of the shape: a
// caller that wants the accounts a person belongs to wants the membership's key
// and its liveness, and none of its columns.
func TestJunction_PredicatesWithoutAProjection(t *testing.T) {
	t.Parallel()

	content := pagedList(For(dialect.Postgres).JunctionListQueries("ListCrewsForPerson", "crews",
		[]string{IDColumn, "name", ArchivedAtColumn},
		&Junction{
			Table:    crewMembersTable,
			Column:   crewColumn,
			OnColumn: IDColumn,
			Columns:  crewMemberColumns(),
			Matches:  []Match{{Column: personColumn}},
		}), Ascending).Content

	// The key the far side carries, and the requirement that the row carrying
	// it has not been archived.
	test.StrContains(t, content, "crew_members.belongs_to_person = sqlc.arg(belongs_to_person)")
	test.StrContains(t, content, "crew_members.archived_at IS NULL")

	// And nothing of the junction in the projection: the only aliases in the
	// statement are the two counts' own.
	test.StrNotContains(t, content, "crew_members.id,")
	test.EqOp(t, 2, strings.Count(content, " AS "))
}

// TestJunction_LivenessIsNotTheArchivedToggle pins the asymmetry between the two
// sides of the join. The listed table's archived_at is what include_archived
// admits rows through; the joined table's is a join condition, and a roster
// asked for archived memberships wants the memberships that ended rather than
// the people who were deleted.
func TestJunction_LivenessIsNotTheArchivedToggle(t *testing.T) {
	t.Parallel()

	content := pagedList(For(dialect.Postgres).JunctionListQueries("ListCrewMembers", crewMembersTable,
		crewMemberColumns(), rosterJunction(), Match{Column: crewColumn}), Ascending).Content

	test.StrContains(t, content, "(COALESCE(sqlc.narg(include_archived), false)::boolean OR crew_members.archived_at IS NULL)")
	test.StrContains(t, content, "AND people.archived_at IS NULL")
	test.StrNotContains(t, content, "OR people.archived_at IS NULL")
}

// TestJunction_LivenessFollowsTheColumnList keeps the join's predicate derived
// the way every other predicate here is: a joined table with no archived_at is
// not asked to be live, because there is nothing to ask.
func TestJunction_LivenessFollowsTheColumnList(t *testing.T) {
	t.Parallel()

	junction := rosterJunction()
	junction.Columns = []string{IDColumn, "name"}

	content := pagedList(For(dialect.Postgres).JunctionListQueries("ListCrewMembers", crewMembersTable,
		crewMemberColumns(), junction, Match{Column: crewColumn}), Ascending).Content

	test.StrNotContains(t, content, "people.archived_at")
}

// TestGenerator_JunctionListAllQuery_CarriesNoPage pins what "unpaged" removes.
// A caller reading every row counts them by looking at what came back, and a
// cursor predicate on a statement with no LIMIT would silently drop the rows
// before the caller's last position.
func TestGenerator_JunctionListAllQuery_CarriesNoPage(T *testing.T) {
	T.Parallel()

	for _, d := range []dialect.Dialect{dialect.Postgres, dialect.MySQL, dialect.SQLite} {
		T.Run(string(d), func(t *testing.T) {
			t.Parallel()

			content := For(d).JunctionListAllQuery("ListCrewsForPerson", crewMembersTable,
				crewMemberColumns(), nil, nil, Match{Column: personColumn}).Content

			for _, absent := range []string{
				CursorArg, LimitArg, IncludeArchivedArg,
				CreatedAfterArg, CreatedBeforeArg, UpdatedAfterArg, UpdatedBeforeArg,
				"filtered_count", "total_count", "LIMIT",
			} {
				test.StrNotContains(t, content, absent, test.Sprintf("statement: %s", content))
			}

			// Archived rows are excluded outright rather than through a flag
			// this statement has no argument for.
			test.StrContains(t, content, "WHERE crew_members.archived_at IS NULL")
		})
	}
}

// TestGenerator_JunctionListAllQuery_OrderIsTheCallers pins that an unnamed
// order renders no ORDER BY at all, rather than one this package chose.
func TestGenerator_JunctionListAllQuery_OrderIsTheCallers(t *testing.T) {
	t.Parallel()

	g := For(dialect.Postgres)

	unordered := g.JunctionListAllQuery("ListCrewsForPerson", crewMembersTable,
		crewMemberColumns(), nil, nil, Match{Column: personColumn}).Content
	test.StrNotContains(t, unordered, "ORDER BY")

	ordered := g.JunctionListAllQuery("ListCrewsForPerson", crewMembersTable,
		crewMemberColumns(), nil, []Order{{Column: CreatedAtColumn, Descending: true}},
		Match{Column: personColumn}).Content
	test.StrContains(t, ordered, "ORDER BY crew_members.created_at DESC;")
}

// TestGenerator_JunctionListAllQuery_JoinsToo pins that the unpaged form is the
// paged one's shape rather than a second statement: it joins, it projects the
// joined columns, and it carries the join's predicates.
func TestGenerator_JunctionListAllQuery_JoinsToo(t *testing.T) {
	t.Parallel()

	content := For(dialect.Postgres).JunctionListAllQuery("ListCrewMembers", crewMembersTable,
		crewMemberColumns(), rosterJunction(), nil, Match{Column: crewColumn}).Content

	test.StrContains(t, content, "JOIN people ON crew_members.belongs_to_person=people.id")
	test.StrContains(t, content, "people.id AS person_id")
	test.StrContains(t, content, "AND people.archived_at IS NULL")
}

// TestGenerator_JunctionListAllQuery_WithNothingToKeyOn renders no WHERE rather
// than a WHERE keyword with nothing after it. Reading every row of a lookup
// table is a query somebody means; a syntax error is not.
func TestGenerator_JunctionListAllQuery_WithNothingToKeyOn(t *testing.T) {
	t.Parallel()

	content := For(dialect.Postgres).JunctionListAllQuery("ListEverything", "lookups",
		[]string{IDColumn, "name"}, nil, nil).Content

	test.StrNotContains(t, content, "WHERE")
	test.EqOp(t, "SELECT\n\tlookups.id,\n\tlookups.name\nFROM lookups;", content)
}

// TestJunction_Rejects covers the misuse this package answers with a panic,
// which is every way a Junction can be spelled that would otherwise render a
// statement quietly missing something the caller asked for.
func TestJunction_Rejects(T *testing.T) {
	T.Parallel()

	cases := map[string]struct {
		junction *Junction
		sentinel error
	}{
		"a table with no columns to join it on": {
			junction: &Junction{Table: peopleTable, Columns: personColumns()},
			sentinel: ErrIncompleteJunction,
		},
		"a join column with no table": {
			junction: &Junction{Column: IDColumn, OnColumn: personColumn},
			sentinel: ErrIncompleteJunction,
		},
		"matches with no table to match them on": {
			junction: &Junction{Matches: []Match{{Column: personColumn}}},
			sentinel: ErrIncompleteJunction,
		},
		"a table name that is not an identifier": {
			junction: &Junction{Table: "people; DROP TABLE people", Column: IDColumn, OnColumn: personColumn},
			sentinel: dialect.ErrInvalidIdentifier,
		},
		"a prefix that is not an identifier": {
			junction: &Junction{Table: peopleTable, Column: IDColumn, OnColumn: personColumn, Prefix: "a b"},
			sentinel: dialect.ErrInvalidIdentifier,
		},
	}

	for name, testCase := range cases {
		T.Run(name, func(t *testing.T) {
			t.Parallel()

			recovered := recoverFrom(t, func() {
				For(dialect.Postgres).JunctionListQueries("ListCrewMembers", crewMembersTable,
					crewMemberColumns(), testCase.junction)
			})

			must.ErrorIs(t, recovered, testCase.sentinel)
		})
	}
}

// TestGenerator_JunctionListAllQuery_RejectsABadOrderColumn keeps the ordering
// held to the same identifier rule as everything else this package
// interpolates.
func TestGenerator_JunctionListAllQuery_RejectsABadOrderColumn(t *testing.T) {
	t.Parallel()

	recovered := recoverFrom(t, func() {
		For(dialect.Postgres).JunctionListAllQuery("ListCrewsForPerson", crewMembersTable,
			crewMemberColumns(), nil, []Order{{Column: "created_at DESC, 1"}},
			Match{Column: personColumn})
	})

	must.ErrorIs(t, recovered, dialect.ErrInvalidIdentifier)
}

// TestOrder_String pins the direction being spelled either way, so that reading
// the statement answers the question rather than sending the reader to the
// server's manual for its default.
func TestOrder_String(t *testing.T) {
	t.Parallel()

	test.EqOp(t, "name ASC", Order{Column: "name"}.String())
	test.EqOp(t, "name DESC", Order{Column: "name", Descending: true}.String())
}

// recoverFrom runs f and returns what it panicked with, failing the test when it
// did not panic at all.
func recoverFrom(tb testing.TB, f func()) (recovered error) {
	tb.Helper()

	defer func() {
		value := recover()
		must.NotNil(tb, value, must.Sprint("expected a panic"))

		err, ok := value.(error)
		must.True(tb, ok, must.Sprintf("panicked with %T rather than an error", value))

		recovered = err
	}()

	f()

	return platformerrors.New("unreachable")
}
