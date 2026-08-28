package querygen

import (
	"fmt"
	"slices"
	"strings"

	platformerrors "github.com/primandproper/platform-go/v13/errors"
)

// LikeEscape is the character a prefix search's pattern escapes wildcards with,
// and the character the emitted ESCAPE clause names.
//
// Deliberately not a backslash. A backslash is itself an escape inside a string
// literal on MySQL and MariaDB unless NO_BACKSLASH_ESCAPES is set, so ESCAPE
// '\' is a syntax error there and ESCAPE '\\' is one on a server that has the
// mode set — there is no spelling of it that is right on both. An exclamation
// mark is ordinary in every dialect's string literal, and [PrefixPattern]
// escapes it in the pattern like any other special character.
const LikeEscape = "!"

// ErrUnknownSearchColumn indicates a prefix search over a column the table does
// not have. The statement it would render names a column three times — the
// pattern match, the ordering, and the cursor — so the mistake is one a reader
// of the generated file would have to catch by eye.
//
// It is a programming error rather than a caller's, and panics like the rest of
// this package's misuse.
var ErrUnknownSearchColumn = platformerrors.New("prefix search names a column the table does not have")

// PrefixSearch is a paged search over the leading characters of one column: the
// column, and the names its statements take.
//
// One column does three jobs here, which is why the shape names it once rather
// than three times. It is what the pattern matches, what the page is ordered
// by, and what the cursor compares against — and those three have to be the
// same column or the walk pages through an order the cursor does not name,
// which skips rows and repeats others with nothing reporting an error. A search
// ordered by id would page in creation order while the caller reads a list
// sorted by name.
type PrefixSearch struct {
	// Column is the column the pattern matches, the page is ordered by, and the
	// cursor pages over. It has to be in the table's column list.
	Column string
	// Name is the paged search's query name and CountName is the count's. Both
	// must be unique across the consumer's whole sqlc package, as every
	// QueryAnnotation.Name must — and so must [DescendingName] of Name, which
	// is what the descending half of the page is emitted under.
	Name      string
	CountName string
}

// PrefixSearchQueries renders a prefix search: a page of rows whose column
// begins with a bound pattern, and the count of everything that pattern
// matches.
//
// It emits a set because a search is one. The standard list carries its two
// counts as scalar subqueries in its own SELECT list, so the page and the
// numbers describing it come from one statement at one moment; that does not
// carry over here, because a search's page is cut by a cursor over the same
// column the pattern filters and the count a caller wants is of everything the
// pattern matched rather than of what is left after the cursor. So the count is
// a separate statement, and it is emitted from the same call as the page it
// counts — a consumer that emitted one and hand-wrote the other would have half
// its search checked by sqlc and half of it not, which is the gap the canonical
// corpus exists to close.
//
// The page is emitted in both directions, under Name and [DescendingName] of
// it, because a search takes a filtering.QueryFilter like any other paged read
// and that filter carries a direction. What the direction means here is this
// statement's own order rather than creation order: a search is ordered by the
// column it searched, so its descending half walks that column backwards —
// which is the reading that keeps the cursor and the ORDER BY agreeing, and the
// only one available to a statement that never orders by the id. The count is
// direction-independent and is emitted once.
//
// The statements share every predicate but one. The count is a page's WHERE
// clause without the cursor, for the same reason filtered_count omits it: a
// count that shrank with every page is a progress bar that never fills.
//
// The cursor predicate is always rendered, and an absent cursor is the first
// page — so the first page and the fiftieth are one statement, the same way the
// standard list's keyset walk is. See [Generator.CursorCondition] for how each
// direction says that.
//
// Archived rows are excluded outright rather than through the include_archived
// toggle a filtered list carries. A prefix search is a lookup — somebody is
// typing a name in order to act on whoever comes back — and a soft-deleted row
// surfacing in one is a deleted account offered up for a new membership. A
// caller who wants archived rows wants a different query rather than a flag on
// this one, which is the same reading the single-row statements take.
//
// matches are the equality predicates the search is keyed on beyond the pattern
// — the tenancy scope, conventionally — and they land in both statements, so a
// page and its count cannot come to disagree about whose rows they are.
//
// It panics rather than returning an error, in the manner of the rest of this
// package: its arguments are string literals in a generator binary. The panic
// value is an error wrapping dialect.ErrInvalidIdentifier, ErrUnknownSearchColumn,
// or ErrDuplicateQueryName.
func (g *Generator) PrefixSearchQueries(table string, columns []string, search PrefixSearch, matches ...Match) []*Query {
	mustIdentifier("table name", table)

	for _, column := range columns {
		mustIdentifier("column name", column)
	}

	mustIdentifier("search column", search.Column)

	if !slices.Contains(columns, search.Column) {
		panic(platformerrors.Wrapf(ErrUnknownSearchColumn, "querygen: table %q column %q", table, search.Column))
	}

	queries := []*Query{
		{
			Annotation: QueryAnnotation{Name: search.Name, Type: ManyType},
			Content:    g.prefixSearchStatement(table, columns, search.Column, Ascending, matches),
		},
		{
			Annotation: QueryAnnotation{Name: DescendingName(search.Name), Type: ManyType},
			Content:    g.prefixSearchStatement(table, columns, search.Column, Descending, matches),
		},
		{
			Annotation: QueryAnnotation{Name: search.CountName, Type: OneType},
			Content:    g.prefixSearchCountStatement(table, columns, search.Column, matches),
		},
	}

	mustBeUniquelyNamed(table, queries)

	return queries
}

// PrefixArg is the sqlc argument a prefix search binds its pattern through,
// derived from the searched column so that a table with two of them names them
// apart.
//
// It is the pattern rather than the prefix, and the distinction is the whole
// reason [PrefixPattern] exists: what the caller has is a literal the user
// typed, and what the statement binds is that literal with its wildcards
// escaped and a trailing % appended.
func PrefixArg(column string) string {
	return column + "_prefix"
}

// PrefixPattern turns a literal prefix into the LIKE pattern the emitted
// statement binds, escaping the two wildcards and the escape character itself.
//
// It is here rather than at each caller because the pattern and the ESCAPE
// clause are one decision written in two places. The clause is rendered above,
// naming [LikeEscape]; a caller escaping with anything else — or not escaping
// at all — leaves a user's typed % or _ a wildcard rather than a character, so
// a prefix of "%" returns every row and one of "a_" matches "ab" as readily as
// "a_". That reads as a working search returning too much rather than as a bug,
// which is why it is not left to be remembered.
//
// The wildcards are escaped rather than the pattern being assembled in SQL,
// because escaping is a fact about the value and the concatenation is not:
// there is no portable spelling of REPLACE nesting across the three dialects,
// and Generator.substringMatch's SQL-side concatenation escapes nothing inside
// the term.
//
// strings.NewReplacer scans the input once and never re-examines what it has
// written, so the escape character's own rule cannot double the escapes the
// other two rules introduce — which a sequence of Replace calls would.
func PrefixPattern(prefix string) string {
	replaced := strings.NewReplacer(
		LikeEscape, LikeEscape+LikeEscape,
		"%", LikeEscape+"%",
		"_", LikeEscape+"_",
	).Replace(prefix)

	return replaced + "%"
}

// prefixSearchStatement renders one direction's page: the table's whole
// projection, the pattern and whatever keys the search, the cursor, and the
// ordering the cursor names.
func (g *Generator) prefixSearchStatement(table string, columns []string, column string, direction Direction, matches []Match) string {
	predicates := append(g.prefixSearchPredicates(table, columns, column, matches), g.cursorCondition(table, column, direction))

	return fmt.Sprintf("SELECT\n\t%s\nFROM %s\nWHERE %s\n%s;",
		strings.Join(QualifyAll(table, columns), ",\n\t"),
		table,
		joinPredicates(predicates, "\t"),
		g.cursorLimitClause(table, column, direction),
	)
}

// prefixSearchCountStatement renders the count beside the page: the same
// predicates, without the cursor.
//
// It counts rows rather than a column, so it needs no id — a table keyed on a
// natural key can still be searched, and COUNT(*) is the same number COUNT(id)
// would have been on one that has one.
func (g *Generator) prefixSearchCountStatement(table string, columns []string, column string, matches []Match) string {
	return fmt.Sprintf("SELECT COUNT(*)\nFROM %s\nWHERE %s;",
		table,
		joinPredicates(g.prefixSearchPredicates(table, columns, column, matches), "\t"),
	)
}

// prefixSearchPredicates is what the page and the count share: live rows,
// whatever keys the search, and the pattern.
//
// Both statements are built from this one slice, so the page and the number
// describing it cannot come to disagree about what was searched — the same
// property filterPredicates gives the standard list and its counts.
func (g *Generator) prefixSearchPredicates(table string, columns []string, column string, matches []Match) []string {
	var predicates []string

	if slices.Contains(columns, ArchivedAtColumn) {
		predicates = append(predicates, Qualify(table, ArchivedAtColumn)+" IS NULL")
	}

	predicates = append(predicates, g.matchPredicates(table, true, matches)...)

	return append(predicates, g.prefixCondition(table, column))
}

// prefixCondition renders the pattern match: a bound pattern, and the ESCAPE
// clause naming the character it was escaped with.
//
// The clause is explicit rather than relying on a server default, because the
// three do not agree on one — and a pattern escaped for a character the server
// does not treat as an escape is a pattern with a stray escape character in it,
// which matches nothing rather than matching too much.
//
// The parentheses are not stylistic and they are not for a server. Every one of
// the three parses this predicate bare; sqlc's SQLite grammar does not, and
// reports `no viable alternative at input 'LIKE'` for any conjunction whose
// second operand is an unparenthesized LIKE ... ESCAPE. Wrapping it costs
// nothing on any dialect and is the difference between a statement the checked
// corpus can hold and one three engines run but only two of them can be checked
// against a schema.
func (g *Generator) prefixCondition(table, column string) string {
	return fmt.Sprintf("(%s LIKE %s ESCAPE '%s')",
		Qualify(table, column), g.prefixPatternArgument(PrefixArg(column)), LikeEscape)
}
