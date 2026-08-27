package querygen

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/primandproper/platform-go/v13/database/dialect"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// The junction list's promises are the ones a string comparison cannot see, and
// they are not the single-table list's promises with a join added. What a server
// has to confirm is that the two sides of the join are treated differently — the
// listed table's archived_at is what include_archived admits rows through, and
// the joined table's is a join condition the flag does not reach — that the
// counts beside the page read over the same joined FROM clause the page does,
// and that the cursor walks the listed table's id rather than the joined one's.
//
// The tables are identity's roster with the names filed off: a membership keying
// a crew to a person, and the people it names. Both directions of the shape are
// exercised, because both exist in this module and they differ in which table is
// listed — a roster is a page of memberships with the member attached, where the
// people in a crew is a page of people reached through memberships.

const (
	crewMembersTable = "crew_members"
	peopleTable      = "people"

	crewColumn   = "belongs_to_crew"
	personColumn = "belongs_to_person"

	testCrew  = "crew_one"
	otherCrew = "crew_two"
)

func crewMemberColumns() []string {
	return []string{
		IDColumn,
		crewColumn,
		personColumn,
		CreatedAtColumn,
		LastUpdatedAtColumn,
		ArchivedAtColumn,
	}
}

func personColumns() []string {
	return []string{IDColumn, "name", CreatedAtColumn, LastUpdatedAtColumn, ArchivedAtColumn}
}

// junctionDDL is the two tables, in each dialect's spelling — the same
// differences conventionalDDL carries, and for the same reasons.
func junctionDDL(d dialect.Dialect) []string {
	switch d {
	case dialect.MySQL:
		return []string{
			`CREATE TABLE people (
				id VARCHAR(64) NOT NULL PRIMARY KEY,
				name VARCHAR(255) NOT NULL,
				created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
				last_updated_at DATETIME NULL,
				archived_at DATETIME NULL
			)`,
			`CREATE TABLE crew_members (
				id VARCHAR(64) NOT NULL PRIMARY KEY,
				belongs_to_crew VARCHAR(64) NOT NULL,
				belongs_to_person VARCHAR(64) NOT NULL,
				created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
				last_updated_at DATETIME NULL,
				archived_at DATETIME NULL
			)`,
		}
	case dialect.SQLite:
		return []string{
			`CREATE TABLE people (
				id TEXT NOT NULL PRIMARY KEY,
				name TEXT NOT NULL,
				created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
				last_updated_at TEXT,
				archived_at TEXT
			)`,
			`CREATE TABLE crew_members (
				id TEXT NOT NULL PRIMARY KEY,
				belongs_to_crew TEXT NOT NULL,
				belongs_to_person TEXT NOT NULL,
				created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
				last_updated_at TEXT,
				archived_at TEXT
			)`,
		}
	// Postgres, which For has already narrowed the alternatives to.
	default:
		return []string{
			`CREATE TABLE people (
				id TEXT NOT NULL PRIMARY KEY,
				name TEXT NOT NULL,
				created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
				last_updated_at TIMESTAMP WITH TIME ZONE,
				archived_at TIMESTAMP WITH TIME ZONE
			)`,
			`CREATE TABLE crew_members (
				id TEXT NOT NULL PRIMARY KEY,
				belongs_to_crew TEXT NOT NULL,
				belongs_to_person TEXT NOT NULL,
				created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
				last_updated_at TIMESTAMP WITH TIME ZONE,
				archived_at TIMESTAMP WITH TIME ZONE
			)`,
		}
	}
}

// junctionQueries is the three statements the suite runs: both directions of the
// paged shape, and the unpaged read of the junction's own rows.
func junctionQueries(d dialect.Dialect) []*Query {
	g := For(d)

	return []*Query{
		g.JunctionListQuery("ListCrewMembers", crewMembersTable, crewMemberColumns(),
			&Junction{
				Table:    peopleTable,
				Column:   IDColumn,
				OnColumn: personColumn,
				Columns:  personColumns(),
				Prefix:   "person",
			},
			Match{Column: crewColumn}),

		g.JunctionListQuery("ListPeopleInCrew", peopleTable, personColumns(),
			&Junction{
				Table:    crewMembersTable,
				Column:   personColumn,
				OnColumn: IDColumn,
				Columns:  crewMemberColumns(),
				Matches:  []Match{{Column: crewColumn}},
			}),

		g.JunctionListAllQuery("ListCrewsForPerson", crewMembersTable, crewMemberColumns(),
			nil,
			[]Order{{Column: crewColumn, Descending: true}},
			Match{Column: personColumn}),
	}
}

// junctionQuery finds one of the three and returns it bound.
func junctionQuery(tb testing.TB, d dialect.Dialect, name string, values map[string]any) (statement string, arguments []any) {
	tb.Helper()

	bound, order := bindArguments(d, named(tb, junctionQueries(d), name).Content)

	return bound, argumentsFor(tb, order, values)
}

// listRoster runs the roster and returns the membership ids, the member names
// beside them, and the two counts.
//
// Every column the assertions do not read is scanned into an any, for the reason
// listWidgets gives: the three drivers hand a stored timestamp back as three
// different Go types, and none of this suite's assertions is about which.
func listRoster(tb testing.TB, ctx context.Context, d dialect.Dialect, db *sql.DB, values map[string]any) (ids, names []string, filtered, total int64) {
	tb.Helper()

	statement, arguments := junctionQuery(tb, d, "ListCrewMembers", values)

	rows, err := db.QueryContext(ctx, statement, arguments...)
	must.NoError(tb, err)

	defer func() { must.NoError(tb, rows.Close()) }()

	for rows.Next() {
		var (
			id, name                                     string
			crew, person, created, updated, archived     any
			personID, personCreated, personUpdated, gone any
			rowFiltered, rowTotal                        int64
		)

		must.NoError(tb, rows.Scan(
			&id, &crew, &person, &created, &updated, &archived,
			&personID, &name, &personCreated, &personUpdated, &gone,
			&rowFiltered, &rowTotal,
		))

		ids = append(ids, id)
		names = append(names, name)
		filtered, total = rowFiltered, rowTotal
	}

	must.NoError(tb, rows.Err())

	return ids, names, filtered, total
}

// listPeopleInCrew runs the other direction, whose projection is the listed
// table's alone.
func listPeopleInCrew(tb testing.TB, ctx context.Context, d dialect.Dialect, db *sql.DB, values map[string]any) (ids []string, filtered, total int64) {
	tb.Helper()

	statement, arguments := junctionQuery(tb, d, "ListPeopleInCrew", values)

	rows, err := db.QueryContext(ctx, statement, arguments...)
	must.NoError(tb, err)

	defer func() { must.NoError(tb, rows.Close()) }()

	for rows.Next() {
		var (
			id                               string
			name, created, updated, archived any
			rowFiltered, rowTotal            int64
		)

		must.NoError(tb, rows.Scan(&id, &name, &created, &updated, &archived, &rowFiltered, &rowTotal))

		ids = append(ids, id)
		filtered, total = rowFiltered, rowTotal
	}

	must.NoError(tb, rows.Err())

	return ids, filtered, total
}

// listCrewsForPerson runs the unpaged read, which carries neither counts nor a
// page.
func listCrewsForPerson(tb testing.TB, ctx context.Context, d dialect.Dialect, db *sql.DB, person string) []string {
	tb.Helper()

	statement, arguments := junctionQuery(tb, d, "ListCrewsForPerson", map[string]any{personColumn: person})

	rows, err := db.QueryContext(ctx, statement, arguments...)
	must.NoError(tb, err)

	defer func() { must.NoError(tb, rows.Close()) }()

	var ids []string

	for rows.Next() {
		var (
			id                                       string
			crew, member, created, updated, archived any
		)

		must.NoError(tb, rows.Scan(&id, &crew, &member, &created, &updated, &archived))

		ids = append(ids, id)
	}

	must.NoError(tb, rows.Err())

	return ids
}

// insertRow writes one row of the junction fixture, leaving created_at to the
// server.
func insertRow(tb testing.TB, ctx context.Context, d dialect.Dialect, db *sql.DB, table string, columns []string, values ...any) {
	tb.Helper()

	_, err := db.ExecContext(ctx, fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		table, strings.Join(columns, ", "), d.Placeholders(1, len(columns))), values...)
	must.NoError(tb, err)
}

// archiveRow soft-deletes one row, from the server's own clock — which is the
// shape the SQLite comparisons this suite makes are lexicographic over.
func archiveRow(tb testing.TB, ctx context.Context, d dialect.Dialect, db *sql.DB, table, id string) {
	tb.Helper()

	_, err := db.ExecContext(ctx, fmt.Sprintf("UPDATE %s SET %s = %s WHERE %s = %s",
		table, ArchivedAtColumn, NowExpression, IDColumn, d.Placeholder(1)), id)
	must.NoError(tb, err)
}

// rosterValues is the filter a roster read is answered under, keyed on one crew.
func rosterValues() map[string]any {
	values := filterDefaults()
	values[crewColumn] = testCrew

	return values
}

func runJunctionSuite(t *testing.T, ctx context.Context, d dialect.Dialect, db *sql.DB) {
	t.Helper()

	for _, statement := range junctionDDL(d) {
		_, err := db.ExecContext(ctx, statement)
		must.NoError(t, err)
	}

	t.Run("every junction statement is one the server accepts", func(t *testing.T) {
		for _, query := range junctionQueries(d) {
			prepare(t, ctx, d, db, query)
		}
	})

	// p_003 is archived, which is what makes m_003 absent from every read below
	// however the archived toggle is set; m_004 is the archived membership the
	// toggle does reach.
	for _, person := range [][]any{
		{"p_001", "ada"},
		{"p_002", "grace"},
		{"p_003", "ghost"},
		{"p_004", "leaver"},
	} {
		insertRow(t, ctx, d, db, peopleTable, []string{IDColumn, "name"}, person...)
	}

	for _, member := range [][]any{
		{"m_001", testCrew, "p_001"},
		{"m_002", testCrew, "p_002"},
		{"m_003", testCrew, "p_003"},
		{"m_004", testCrew, "p_004"},
		{"m_005", otherCrew, "p_001"},
	} {
		insertRow(t, ctx, d, db, crewMembersTable, []string{IDColumn, crewColumn, personColumn}, member...)
	}

	archiveRow(t, ctx, d, db, peopleTable, "p_003")
	archiveRow(t, ctx, d, db, crewMembersTable, "m_004")

	t.Run("the roster projects both tables in one read", func(t *testing.T) {
		ids, names, filtered, total := listRoster(t, ctx, d, db, rosterValues())

		test.Eq(t, []string{"m_001", "m_002"}, ids)
		test.Eq(t, []string{"ada", "grace"}, names)
		test.EqOp(t, int64(2), filtered)
		test.EqOp(t, int64(2), total)
	})

	t.Run("include_archived reaches the listed table and not the joined one", func(t *testing.T) {
		values := rosterValues()
		values[IncludeArchivedArg] = true

		ids, _, filtered, total := listRoster(t, ctx, d, db, values)

		// m_004 is the archived membership the flag admits. m_003 is the live
		// membership of an archived person, and it stays out: the flag says
		// which rows are being listed, and the joined row is a reference those
		// rows hold rather than one of them.
		test.Eq(t, []string{"m_001", "m_002", "m_004"}, ids)
		test.EqOp(t, int64(3), filtered)
		test.EqOp(t, int64(3), total)
	})

	t.Run("the counts read over the joined FROM clause", func(t *testing.T) {
		// Four live memberships name this crew and only two survive the join,
		// so a count taken over the listed table alone would report four here.
		_, _, filtered, total := listRoster(t, ctx, d, db, rosterValues())

		test.EqOp(t, int64(2), filtered)
		test.EqOp(t, int64(2), total)
	})

	t.Run("the cursor walks the listed table and the counts do not move", func(t *testing.T) {
		values := rosterValues()
		values[LimitArg] = 1

		first, _, filtered, total := listRoster(t, ctx, d, db, values)
		test.Eq(t, []string{"m_001"}, first)
		test.EqOp(t, int64(2), filtered)
		test.EqOp(t, int64(2), total)

		values[CursorArg] = first[len(first)-1]

		second, names, filteredAgain, totalAgain := listRoster(t, ctx, d, db, values)
		test.Eq(t, []string{"m_002"}, second)
		test.Eq(t, []string{"grace"}, names)

		test.EqOp(t, filtered, filteredAgain)
		test.EqOp(t, total, totalAgain)
	})

	t.Run("the other direction lists the far table keyed on the junction", func(t *testing.T) {
		values := filterDefaults()
		values[crewColumn] = testCrew

		ids, filtered, total := listPeopleInCrew(t, ctx, d, db, values)

		// The archived person is excluded by the listed table's own toggle here,
		// and the archived membership by the join's liveness — the two swap
		// roles when the direction does.
		test.Eq(t, []string{"p_001", "p_002"}, ids)
		test.EqOp(t, int64(2), filtered)
		test.EqOp(t, int64(2), total)
	})

	t.Run("the unpaged read returns every live row in the order it names", func(t *testing.T) {
		// Descending by crew, which is the reverse of the id order these two
		// rows were written in — so an ordering the statement did not carry
		// would be visible here rather than coincidentally right.
		test.Eq(t, []string{"m_005", "m_001"}, listCrewsForPerson(t, ctx, d, db, "p_001"))

		// And no flag admits an archived row to it.
		test.SliceEmpty(t, listCrewsForPerson(t, ctx, d, db, "p_004"))
	})
}
